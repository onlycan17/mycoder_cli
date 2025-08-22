package planner

import (
	"regexp"
	"strings"
)

type Intent string

const (
	IntentUnknown  Intent = "unknown"
	IntentNavigate Intent = "nav"
	IntentExplain  Intent = "explain"
	IntentEdit     Intent = "edit"
	IntentResearch Intent = "research"
)

var (
	reNav      = regexp.MustCompile(`(?i)where\s+is|find\s+(?:file|symbol)|navigate|go\s+to`)
	reExplain  = regexp.MustCompile(`(?i)explain|what\s+does|요약|설명`)
	reEdit     = regexp.MustCompile(`(?i)edit|refactor|rename|change|modify|fix|추가|수정|변경`)
	reResearch = regexp.MustCompile(`(?i)compare|alternatives|research|pros\s+and\s+cons|장단점|조사`)
)

// Classify returns a coarse intent for a user query.
func Classify(q string) Intent {
	s := strings.TrimSpace(q)
	if s == "" {
		return IntentUnknown
	}
	if reEdit.MatchString(s) {
		return IntentEdit
	}
	if reExplain.MatchString(s) {
		return IntentExplain
	}
	if reNav.MatchString(s) {
		return IntentNavigate
	}
	if reResearch.MatchString(s) {
		return IntentResearch
	}
	// heuristic fallback: code-looking tokens -> explain/edit bias
	if strings.Contains(s, "func ") || strings.Contains(s, "class ") || strings.Contains(s, "def ") {
		return IntentExplain
	}
	return IntentUnknown
}

// RetrievalK returns a K recommendation by intent given a base K.
func RetrievalK(intent Intent, base int) int {
	if base <= 0 {
		base = 5
	}
	switch intent {
	case IntentNavigate:
		return base // focused
	case IntentExplain:
		if base < 7 {
			return 7
		}
		return base
	case IntentEdit:
		if base < 8 {
			return 8
		}
		return base
	case IntentResearch:
		if base < 10 {
			return 10
		}
		return base
	default:
		return base
	}
}
