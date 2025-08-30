package opensearch

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type JapaneseTextProcessor struct {
	stopWords        []string
	kanaRegex        *regexp.Regexp
	kanjiRegex       *regexp.Regexp
	alphaNumRegex    *regexp.Regexp
	whitespaceRegex  *regexp.Regexp
	punctuationRegex *regexp.Regexp
}

type ProcessedQuery struct {
	Original     string   `json:"original"`
	Normalized   string   `json:"normalized"`
	Terms        []string `json:"terms"`
	JapaneseText string   `json:"japanese_text"`
	AlphaNumeric string   `json:"alpha_numeric"`
	Language     string   `json:"language"`
}

func NewJapaneseTextProcessor() *JapaneseTextProcessor {
	return &JapaneseTextProcessor{
		stopWords: []string{
			"の", "に", "は", "を", "が", "で", "と", "から", "まで", "より", "へ",
			"か", "も", "や", "て", "だ", "である", "です", "ます", "した", "する",
			"した", "して", "いる", "ある", "ない", "なく", "という", "こと",
			"これ", "それ", "あれ", "この", "その", "あの", "ここ", "そこ", "あそこ",
			"どこ", "だれ", "なに", "なん", "いつ", "どう", "なぜ", "どのような",
		},
		kanaRegex:        regexp.MustCompile(`[\p{Hiragana}\p{Katakana}ー]+`),
		kanjiRegex:       regexp.MustCompile(`[\p{Han}]+`),
		alphaNumRegex:    regexp.MustCompile(`[A-Za-z0-9\-_\.]+`),
		whitespaceRegex:  regexp.MustCompile(`\s+`),
		punctuationRegex: regexp.MustCompile(`[、。！？，．：；（）「」『』【】〈〉《》〔〕［］｛｝]+`),
	}
}

func (jp *JapaneseTextProcessor) ProcessQuery(query string) *ProcessedQuery {
	if query == "" {
		return &ProcessedQuery{
			Original:   "",
			Normalized: "",
			Terms:      []string{},
			Language:   "unknown",
		}
	}

	result := &ProcessedQuery{
		Original: query,
	}

	normalized := jp.normalizeUnicode(query)
	normalized = jp.sanitizeQuery(normalized)
	result.Normalized = normalized

	language := jp.detectLanguage(normalized)
	result.Language = language

	japaneseText := jp.extractJapaneseText(normalized)
	result.JapaneseText = japaneseText

	alphaNumeric := jp.extractAlphaNumeric(normalized)
	result.AlphaNumeric = alphaNumeric

	terms := jp.tokenizeQuery(normalized)
	terms = jp.removeStopWords(terms)
	terms = jp.filterEmptyTerms(terms)
	result.Terms = terms

	return result
}

func (jp *JapaneseTextProcessor) normalizeUnicode(text string) string {
	var result strings.Builder

	for _, r := range text {
		switch {
		case r >= 'Ａ' && r <= 'Ｚ':
			result.WriteRune(r - 'Ａ' + 'A')
		case r >= 'ａ' && r <= 'ｚ':
			result.WriteRune(r - 'ａ' + 'a')
		case r >= '０' && r <= '９':
			result.WriteRune(r - '０' + '0')
		case r == '　':
			result.WriteRune(' ')
		case r >= 'ァ' && r <= 'ヶ':
			if hiragana := jp.katakanaToHiragana(r); hiragana != r {
				result.WriteRune(hiragana)
			} else {
				result.WriteRune(r)
			}
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}

func (jp *JapaneseTextProcessor) katakanaToHiragana(r rune) rune {
	if r >= 'ァ' && r <= 'ヶ' {
		if r == 'ヶ' {
			return 'か'
		}
		if r == 'ヵ' {
			return 'か'
		}
		return r - 'ァ' + 'ぁ'
	}
	return r
}

func (jp *JapaneseTextProcessor) sanitizeQuery(query string) string {
	query = jp.punctuationRegex.ReplaceAllString(query, " ")

	query = strings.ReplaceAll(query, "\n", " ")
	query = strings.ReplaceAll(query, "\t", " ")
	query = strings.ReplaceAll(query, "\r", " ")

	query = jp.whitespaceRegex.ReplaceAllString(query, " ")

	query = strings.TrimSpace(query)

	return query
}

func (jp *JapaneseTextProcessor) detectLanguage(text string) string {
	if text == "" {
		return "unknown"
	}

	japaneseChars := 0
	alphaNumChars := 0
	totalChars := utf8.RuneCountInString(text)

	for _, r := range text {
		if jp.isJapaneseChar(r) {
			japaneseChars++
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alphaNumChars++
		}
	}

	japaneseRatio := float64(japaneseChars) / float64(totalChars)
	alphaNumRatio := float64(alphaNumChars) / float64(totalChars)

	if japaneseRatio > 0.3 && alphaNumRatio > 0.2 {
		return "mixed"
	} else if japaneseRatio > 0.5 {
		return "japanese"
	} else if alphaNumRatio > 0.7 {
		return "english"
	}

	return "mixed"
}

func (jp *JapaneseTextProcessor) isJapaneseChar(r rune) bool {
	return (r >= 0x3040 && r <= 0x309F) ||
		(r >= 0x30A0 && r <= 0x30FF) ||
		(r >= 0x4E00 && r <= 0x9FAF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		r == 0x30FC
}

func (jp *JapaneseTextProcessor) extractJapaneseText(text string) string {
	matches := jp.kanaRegex.FindAllString(text, -1)
	kanjiMatches := jp.kanjiRegex.FindAllString(text, -1)

	allMatches := append(matches, kanjiMatches...)

	return strings.Join(allMatches, " ")
}

func (jp *JapaneseTextProcessor) extractAlphaNumeric(text string) string {
	matches := jp.alphaNumRegex.FindAllString(text, -1)
	return strings.Join(matches, " ")
}

func (jp *JapaneseTextProcessor) tokenizeQuery(query string) []string {
	query = strings.ToLower(query)

	var tokens []string

	japaneseText := jp.extractJapaneseText(query)
	if japaneseText != "" {
		japaneseTokens := jp.tokenizeJapanese(japaneseText)
		tokens = append(tokens, japaneseTokens...)
	}

	alphaNumText := jp.extractAlphaNumeric(query)
	if alphaNumText != "" {
		alphaNumTokens := strings.Fields(alphaNumText)
		tokens = append(tokens, alphaNumTokens...)
	}

	return tokens
}

func (jp *JapaneseTextProcessor) tokenizeJapanese(text string) []string {
	words := strings.Fields(text)
	var tokens []string

	for _, word := range words {
		if len(word) > 0 {
			tokens = append(tokens, word)

			if utf8.RuneCountInString(word) > 2 {
				for i := 0; i < len(word)-1; i++ {
					if r1, size1 := utf8.DecodeRuneInString(word[i:]); size1 > 0 {
						if r2, size2 := utf8.DecodeRuneInString(word[i+size1:]); size2 > 0 {
							bigram := string(r1) + string(r2)
							tokens = append(tokens, bigram)
						}
					}
				}
			}
		}
	}

	return tokens
}

func (jp *JapaneseTextProcessor) removeStopWords(terms []string) []string {
	stopWordSet := make(map[string]bool)
	for _, word := range jp.stopWords {
		stopWordSet[word] = true
	}

	var filtered []string
	for _, term := range terms {
		if !stopWordSet[term] && len(term) > 1 {
			filtered = append(filtered, term)
		}
	}

	return filtered
}

func (jp *JapaneseTextProcessor) filterEmptyTerms(terms []string) []string {
	var filtered []string
	for _, term := range terms {
		trimmed := strings.TrimSpace(term)
		if trimmed != "" && utf8.RuneCountInString(trimmed) > 0 {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}

func (jp *JapaneseTextProcessor) BuildOptimizedQuery(processedQuery *ProcessedQuery, fields []string) map[string]interface{} {
	if len(processedQuery.Terms) == 0 {
		return map[string]interface{}{
			"match_all": map[string]interface{}{},
		}
	}

	switch processedQuery.Language {
	case "japanese":
		return jp.buildJapaneseQuery(processedQuery, fields)
	case "english":
		return jp.buildEnglishQuery(processedQuery, fields)
	case "mixed":
		return jp.buildMixedQuery(processedQuery, fields)
	default:
		return jp.buildDefaultQuery(processedQuery, fields)
	}
}

func (jp *JapaneseTextProcessor) buildJapaneseQuery(processedQuery *ProcessedQuery, fields []string) map[string]interface{} {
	return map[string]interface{}{
		"multi_match": map[string]interface{}{
			"query":     processedQuery.Normalized,
			"fields":    fields,
			"type":      "best_fields",
			"operator":  "and",
			"analyzer":  "kuromoji",
			"fuzziness": "AUTO",
		},
	}
}

func (jp *JapaneseTextProcessor) buildEnglishQuery(processedQuery *ProcessedQuery, fields []string) map[string]interface{} {
	return map[string]interface{}{
		"multi_match": map[string]interface{}{
			"query":     processedQuery.Normalized,
			"fields":    fields,
			"type":      "best_fields",
			"operator":  "and",
			"fuzziness": "AUTO",
		},
	}
}

func (jp *JapaneseTextProcessor) buildMixedQuery(processedQuery *ProcessedQuery, fields []string) map[string]interface{} {
	queries := []map[string]interface{}{}

	if processedQuery.JapaneseText != "" {
		queries = append(queries, map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    processedQuery.JapaneseText,
				"fields":   fields,
				"type":     "best_fields",
				"analyzer": "kuromoji",
				"boost":    1.2,
			},
		})
	}

	if processedQuery.AlphaNumeric != "" {
		queries = append(queries, map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":     processedQuery.AlphaNumeric,
				"fields":    fields,
				"type":      "best_fields",
				"fuzziness": "AUTO",
				"boost":     1.0,
			},
		})
	}

	if len(queries) == 1 {
		return queries[0]
	}

	return map[string]interface{}{
		"bool": map[string]interface{}{
			"should":               queries,
			"minimum_should_match": 1,
		},
	}
}

func (jp *JapaneseTextProcessor) buildDefaultQuery(processedQuery *ProcessedQuery, fields []string) map[string]interface{} {
	return map[string]interface{}{
		"multi_match": map[string]interface{}{
			"query":  strings.Join(processedQuery.Terms, " "),
			"fields": fields,
			"type":   "best_fields",
		},
	}
}
