package slacksearch

import (
	"regexp"
	"strings"
)

// SlackSearchDirective represents a user's explicit intent regarding Slack search.
type SlackSearchDirective int

const (
	// SlackSearchUnspecified means the query contains no explicit Slack search directive.
	SlackSearchUnspecified SlackSearchDirective = iota
	// SlackSearchExplicitEnable means the user explicitly requested Slack search.
	SlackSearchExplicitEnable
	// SlackSearchExplicitDisable means the user explicitly requested to skip Slack search.
	SlackSearchExplicitDisable
)

// DirectiveResult contains the parsed directive and a cleaned query with directive text removed.
type DirectiveResult struct {
	Directive    SlackSearchDirective
	CleanedQuery string
}

// Regex: "Slack検索を利用せず", "Slack検索なしで", "Slackは使わず", "without slack search", etc.
var disablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*を?\s*(?:利用|使用)\s*(?:せず|しない(?:で)?)`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*(?:なし|無し|オフ|off)\s*(?:で|に(?:して)?)?`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*を?\s*(?:除い|外し|省い|スキップし)\s*て`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*(?:会話|検索)\s*(?:は|を)\s*(?:検索|利用|使用)\s*しない(?:で)?`),
	regexp.MustCompile(`(?i)slack\s*(?:を|は)\s*(?:使わ|利用し)\s*(?:ず|ない(?:で)?)`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*(?:を)?\s*(?:無効|ディスエーブル|disable)`),
	regexp.MustCompile(`(?i)(?:without|skip(?:ping)?|disable|exclude|no)\s+slack\s*(?:search)?`),
	regexp.MustCompile(`(?i)(?:don'?t|do\s+not)\s+(?:use|search|include)\s+slack`),
	regexp.MustCompile(`(?i)slack\s*search\s*(?:off|disabled?)`),
}

// Regex: "Slack検索を利用して", "Slackも検索して", "with slack search", etc.
var enablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*を?\s*(?:利用|使用)\s*(?:して|する)`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*(?:会話\s*)?(?:も|を)\s*(?:検索し|含め)\s*(?:て|る)`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*(?:あり|オン|on)\s*(?:で|に(?:して)?)?`),
	regexp.MustCompile(`(?i)slack\s*(?:の)?\s*検索\s*(?:を)?\s*(?:有効|イネーブル|enable)`),
	regexp.MustCompile(`(?i)(?:with|include|enable|use)\s+slack\s*(?:search)?`),
	regexp.MustCompile(`(?i)(?:also|and)\s+(?:search|check)\s+slack`),
	regexp.MustCompile(`(?i)slack\s*search\s*(?:on|enabled?)`),
}

// DetectSlackSearchDirective analyzes the query for explicit Slack search directives.
// Disable patterns are checked first; if the user says "don't use Slack search"
// the result is ExplicitDisable even if the query also contains the word "Slack".
func DetectSlackSearchDirective(query string) DirectiveResult {
	if query == "" {
		return DirectiveResult{Directive: SlackSearchUnspecified, CleanedQuery: query}
	}

	normalized := normalizeFullWidth(query)

	for _, pat := range disablePatterns {
		if loc := pat.FindStringIndex(normalized); loc != nil {
			cleaned := stripMatchedDirective(query, normalized, loc)
			return DirectiveResult{Directive: SlackSearchExplicitDisable, CleanedQuery: cleaned}
		}
	}

	for _, pat := range enablePatterns {
		if loc := pat.FindStringIndex(normalized); loc != nil {
			cleaned := stripMatchedDirective(query, normalized, loc)
			return DirectiveResult{Directive: SlackSearchExplicitEnable, CleanedQuery: cleaned}
		}
	}

	return DirectiveResult{Directive: SlackSearchUnspecified, CleanedQuery: query}
}

func normalizeFullWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '\uFF01' && r <= '\uFF5E' {
			b.WriteRune(r - 0xFEE0)
		} else if r == '\u3000' {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripMatchedDirective(original, normalized string, loc []int) string {
	origRunes := []rune(original)
	startRune := byteOffsetToRuneOffset(normalized, loc[0])
	endRune := byteOffsetToRuneOffset(normalized, loc[1])

	if startRune < 0 || endRune < 0 || endRune > len(origRunes) {
		return original
	}

	before := string(origRunes[:startRune])
	after := string(origRunes[endRune:])

	after = strings.TrimLeft(after, "、,。. ")
	before = strings.TrimRight(before, "、,。. ")

	result := strings.TrimSpace(before + " " + after)
	if result == "" {
		return original
	}
	return result
}

func byteOffsetToRuneOffset(s string, byteOffset int) int {
	runeIndex := 0
	for i := range s {
		if i == byteOffset {
			return runeIndex
		}
		runeIndex++
	}
	if byteOffset == len(s) {
		return runeIndex
	}
	return -1
}
