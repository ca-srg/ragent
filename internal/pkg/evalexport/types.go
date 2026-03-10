package evalexport

import (
	"time"

	"github.com/google/uuid"
)

const schemaVersion = "1.0"

type EvalRecord struct {
	SchemaVersion     string            `json:"schema_version"`
	ID                string            `json:"id"`
	Timestamp         time.Time         `json:"timestamp"`
	Command           string            `json:"command"`
	UserInput         string            `json:"user_input"`
	RetrievedContexts []string          `json:"retrieved_contexts"`
	Response          string            `json:"response"`
	RetrievedDocs     []RetrievedDoc    `json:"retrieved_docs"`
	RunConfig         RunConfig         `json:"run_config"`
	Timing            Timing            `json:"timing"`
	References        map[string]string `json:"references"`
}

type RetrievedDoc struct {
	DocID       string  `json:"doc_id"`
	Rank        int     `json:"rank"`
	Text        string  `json:"text"`
	FusedScore  float64 `json:"fused_score"`
	BM25Score   float64 `json:"bm25_score"`
	VectorScore float64 `json:"vector_score"`
	SearchType  string  `json:"search_type"`
	SourceFile  string  `json:"source_file"`
	Title       string  `json:"title"`
}

type RunConfig struct {
	SearchMode         string  `json:"search_mode"`
	BM25Weight         float64 `json:"bm25_weight"`
	VectorWeight       float64 `json:"vector_weight"`
	FusionMethod       string  `json:"fusion_method"`
	TopK               int     `json:"top_k"`
	IndexName          string  `json:"index_name"`
	UseJapaneseNLP     bool    `json:"use_japanese_nlp"`
	ChatModel          string  `json:"chat_model"`
	EmbeddingModel     string  `json:"embedding_model"`
	SlackSearchEnabled bool    `json:"slack_search_enabled"`
}

type Timing struct {
	TotalMs     int64 `json:"total_ms"`
	EmbeddingMs int64 `json:"embedding_ms"`
	BM25Ms      int64 `json:"bm25_ms"`
	VectorMs    int64 `json:"vector_ms"`
	FusionMs    int64 `json:"fusion_ms"`
	LLMMs       int64 `json:"llm_ms"`
}

func NewEvalRecord(command, userInput string) *EvalRecord {
	return &EvalRecord{
		SchemaVersion:     schemaVersion,
		ID:                uuid.New().String(),
		Timestamp:         time.Now().UTC(),
		Command:           command,
		UserInput:         userInput,
		RetrievedContexts: []string{},
		RetrievedDocs:     []RetrievedDoc{},
		References:        map[string]string{},
	}
}
