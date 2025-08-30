package opensearch

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

type FusionEngine struct {
	rankConstant float64
}

type ScoredDoc struct {
	ID          string          `json:"id"`
	Score       float64         `json:"score"`
	BM25Score   float64         `json:"bm25_score,omitempty"`
	VectorScore float64         `json:"vector_score,omitempty"`
	FusedScore  float64         `json:"fused_score"`
	Source      json.RawMessage `json:"source"`
	Index       string          `json:"index"`
	Rank        int             `json:"rank"`
	SearchType  string          `json:"search_type"`
}

type FusionResult struct {
	Documents     []ScoredDoc `json:"documents"`
	TotalHits     int         `json:"total_hits"`
	MaxScore      float64     `json:"max_score"`
	BM25Results   int         `json:"bm25_results"`
	VectorResults int         `json:"vector_results"`
	FusionType    string      `json:"fusion_type"`
}

type FusionMethod string

const (
	FusionMethodRRF         FusionMethod = "rrf"
	FusionMethodWeightedSum FusionMethod = "weighted_sum"
	FusionMethodMaxScore    FusionMethod = "max_score"
)

func NewFusionEngine(rankConstant float64) *FusionEngine {
	if rankConstant <= 0 {
		rankConstant = 60.0
	}
	return &FusionEngine{
		rankConstant: rankConstant,
	}
}

func (fe *FusionEngine) FuseResults(bm25Results *BM25SearchResponse, vectorResults *VectorSearchResponse, method FusionMethod, bm25Weight, vectorWeight float64) (*FusionResult, error) {
	if bm25Results == nil && vectorResults == nil {
		return nil, fmt.Errorf("at least one search result must be provided")
	}

	bm25Docs := fe.convertBM25Results(bm25Results)
	vectorDocs := fe.convertVectorResults(vectorResults)

	switch method {
	case FusionMethodRRF:
		return fe.fuseWithRRF(bm25Docs, vectorDocs)
	case FusionMethodWeightedSum:
		return fe.fuseWithWeightedSum(bm25Docs, vectorDocs, bm25Weight, vectorWeight)
	case FusionMethodMaxScore:
		return fe.fuseWithMaxScore(bm25Docs, vectorDocs)
	default:
		return fe.fuseWithRRF(bm25Docs, vectorDocs)
	}
}

func (fe *FusionEngine) convertBM25Results(results *BM25SearchResponse) []ScoredDoc {
	if results == nil || len(results.Hits.Hits) == 0 {
		return []ScoredDoc{}
	}

	docs := make([]ScoredDoc, len(results.Hits.Hits))
	for i, hit := range results.Hits.Hits {
		docs[i] = ScoredDoc{
			ID:         hit.ID,
			Score:      hit.Score,
			BM25Score:  hit.Score,
			Source:     hit.Source,
			Index:      hit.Index,
			Rank:       i + 1,
			SearchType: "bm25",
		}
	}
	return docs
}

func (fe *FusionEngine) convertVectorResults(results *VectorSearchResponse) []ScoredDoc {
	if results == nil || len(results.Hits.Hits) == 0 {
		return []ScoredDoc{}
	}

	docs := make([]ScoredDoc, len(results.Hits.Hits))
	for i, hit := range results.Hits.Hits {
		docs[i] = ScoredDoc{
			ID:          hit.ID,
			Score:       hit.Score,
			VectorScore: hit.Score,
			Source:      hit.Source,
			Index:       hit.Index,
			Rank:        i + 1,
			SearchType:  "vector",
		}
	}
	return docs
}

func (fe *FusionEngine) fuseWithRRF(bm25Docs, vectorDocs []ScoredDoc) (*FusionResult, error) {
	docMap := make(map[string]*ScoredDoc)

	for _, doc := range bm25Docs {
		docCopy := doc
		rrfScore := 1.0 / (fe.rankConstant + float64(doc.Rank))
		docCopy.FusedScore = rrfScore
		docMap[doc.ID] = &docCopy
	}

	for _, doc := range vectorDocs {
		rrfScore := 1.0 / (fe.rankConstant + float64(doc.Rank))

		if existing, exists := docMap[doc.ID]; exists {
			existing.FusedScore += rrfScore
			existing.VectorScore = doc.VectorScore
			existing.SearchType = "hybrid"
		} else {
			docCopy := doc
			docCopy.FusedScore = rrfScore
			docMap[doc.ID] = &docCopy
		}
	}

	fusedDocs := make([]ScoredDoc, 0, len(docMap))
	maxScore := 0.0

	for _, doc := range docMap {
		if doc.FusedScore > maxScore {
			maxScore = doc.FusedScore
		}
		fusedDocs = append(fusedDocs, *doc)
	}

	sort.Slice(fusedDocs, func(i, j int) bool {
		return fusedDocs[i].FusedScore > fusedDocs[j].FusedScore
	})

	for i := range fusedDocs {
		fusedDocs[i].Rank = i + 1
	}

	return &FusionResult{
		Documents:     fusedDocs,
		TotalHits:     len(fusedDocs),
		MaxScore:      maxScore,
		BM25Results:   len(bm25Docs),
		VectorResults: len(vectorDocs),
		FusionType:    string(FusionMethodRRF),
	}, nil
}

func (fe *FusionEngine) fuseWithWeightedSum(bm25Docs, vectorDocs []ScoredDoc, bm25Weight, vectorWeight float64) (*FusionResult, error) {
	if bm25Weight < 0 || vectorWeight < 0 {
		return nil, fmt.Errorf("weights must be non-negative")
	}

	if bm25Weight == 0 && vectorWeight == 0 {
		bm25Weight, vectorWeight = 0.5, 0.5
	}

	totalWeight := bm25Weight + vectorWeight
	bm25Weight = bm25Weight / totalWeight
	vectorWeight = vectorWeight / totalWeight

	docMap := make(map[string]*ScoredDoc)

	bm25Max, vectorMax := fe.findMaxScores(bm25Docs, vectorDocs)

	for _, doc := range bm25Docs {
		docCopy := doc
		normalizedScore := doc.BM25Score / bm25Max
		docCopy.FusedScore = normalizedScore * bm25Weight
		docMap[doc.ID] = &docCopy
	}

	for _, doc := range vectorDocs {
		normalizedScore := doc.VectorScore / vectorMax
		weightedScore := normalizedScore * vectorWeight

		if existing, exists := docMap[doc.ID]; exists {
			existing.FusedScore += weightedScore
			existing.VectorScore = doc.VectorScore
			existing.SearchType = "hybrid"
		} else {
			docCopy := doc
			docCopy.FusedScore = weightedScore
			docMap[doc.ID] = &docCopy
		}
	}

	fusedDocs := make([]ScoredDoc, 0, len(docMap))
	maxScore := 0.0

	for _, doc := range docMap {
		if doc.FusedScore > maxScore {
			maxScore = doc.FusedScore
		}
		fusedDocs = append(fusedDocs, *doc)
	}

	sort.Slice(fusedDocs, func(i, j int) bool {
		return fusedDocs[i].FusedScore > fusedDocs[j].FusedScore
	})

	for i := range fusedDocs {
		fusedDocs[i].Rank = i + 1
	}

	return &FusionResult{
		Documents:     fusedDocs,
		TotalHits:     len(fusedDocs),
		MaxScore:      maxScore,
		BM25Results:   len(bm25Docs),
		VectorResults: len(vectorDocs),
		FusionType:    string(FusionMethodWeightedSum),
	}, nil
}

func (fe *FusionEngine) fuseWithMaxScore(bm25Docs, vectorDocs []ScoredDoc) (*FusionResult, error) {
	docMap := make(map[string]*ScoredDoc)

	bm25Max, vectorMax := fe.findMaxScores(bm25Docs, vectorDocs)

	for _, doc := range bm25Docs {
		docCopy := doc
		docCopy.FusedScore = doc.BM25Score / bm25Max
		docMap[doc.ID] = &docCopy
	}

	for _, doc := range vectorDocs {
		normalizedScore := doc.VectorScore / vectorMax

		if existing, exists := docMap[doc.ID]; exists {
			existing.FusedScore = math.Max(existing.FusedScore, normalizedScore)
			existing.VectorScore = doc.VectorScore
			existing.SearchType = "hybrid"
		} else {
			docCopy := doc
			docCopy.FusedScore = normalizedScore
			docMap[doc.ID] = &docCopy
		}
	}

	fusedDocs := make([]ScoredDoc, 0, len(docMap))
	maxScore := 0.0

	for _, doc := range docMap {
		if doc.FusedScore > maxScore {
			maxScore = doc.FusedScore
		}
		fusedDocs = append(fusedDocs, *doc)
	}

	sort.Slice(fusedDocs, func(i, j int) bool {
		return fusedDocs[i].FusedScore > fusedDocs[j].FusedScore
	})

	for i := range fusedDocs {
		fusedDocs[i].Rank = i + 1
	}

	return &FusionResult{
		Documents:     fusedDocs,
		TotalHits:     len(fusedDocs),
		MaxScore:      maxScore,
		BM25Results:   len(bm25Docs),
		VectorResults: len(vectorDocs),
		FusionType:    string(FusionMethodMaxScore),
	}, nil
}

func (fe *FusionEngine) findMaxScores(bm25Docs, vectorDocs []ScoredDoc) (float64, float64) {
	bm25Max := 0.0
	vectorMax := 0.0

	for _, doc := range bm25Docs {
		if doc.BM25Score > bm25Max {
			bm25Max = doc.BM25Score
		}
	}

	for _, doc := range vectorDocs {
		if doc.VectorScore > vectorMax {
			vectorMax = doc.VectorScore
		}
	}

	if bm25Max == 0 {
		bm25Max = 1.0
	}
	if vectorMax == 0 {
		vectorMax = 1.0
	}

	return bm25Max, vectorMax
}

func (fe *FusionEngine) FilterDuplicates(docs []ScoredDoc) []ScoredDoc {
	seen := make(map[string]bool)
	var unique []ScoredDoc

	for _, doc := range docs {
		if !seen[doc.ID] {
			seen[doc.ID] = true
			unique = append(unique, doc)
		}
	}

	return unique
}

func (fe *FusionEngine) RankResults(docs []ScoredDoc) []ScoredDoc {
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].FusedScore > docs[j].FusedScore
	})

	for i := range docs {
		docs[i].Rank = i + 1
	}

	return docs
}

func (fe *FusionEngine) ApplyThreshold(docs []ScoredDoc, minScore float64) []ScoredDoc {
	if minScore <= 0 {
		return docs
	}

	var filtered []ScoredDoc
	for _, doc := range docs {
		if doc.FusedScore >= minScore {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

func (fe *FusionEngine) LimitResults(docs []ScoredDoc, limit int) []ScoredDoc {
	if limit <= 0 || limit >= len(docs) {
		return docs
	}

	return docs[:limit]
}
