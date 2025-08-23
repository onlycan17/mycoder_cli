package patch

import (
	"errors"
	"fmt"
	"strings"
)

// ApplyToContent applies unified hunks to the original content.
// It returns the new content, number of added/deleted lines, and a conflict error if any mismatch occurs.
func ApplyToContent(original string, hunks []UnifiedHunk) (string, int, int, error) {
	return ApplyToContentOpt(original, hunks, ApplyOptions{})
}

// ApplyOptions controls loose comparisons when applying hunks.
type ApplyOptions struct {
	IgnoreWhitespace bool
}

// ApplyToContentOpt applies hunks with options.
func ApplyToContentOpt(original string, hunks []UnifiedHunk, opt ApplyOptions) (string, int, int, error) {
	// split into lines without dropping trailing last empty line
	// We operate on logical lines without the trailing '\n'. We'll rejoin with '\n'.
	src := splitLines(original)
	var out []string
	cur := 1 // 1-based
	totalAdd, totalDel := 0, 0
	for _, h := range hunks {
		// copy unchanged up to h.OldStart-1
		if h.OldStart < cur {
			return "", 0, 0, fmt.Errorf("overlapping hunks or invalid hunk start: have %d need %d", cur, h.OldStart)
		}
		for cur <= len(src) && cur < h.OldStart {
			out = append(out, src[cur-1])
			cur++
		}
		// apply hunk lines
		for _, ln := range h.Lines {
			switch ln.Kind {
			case Context:
				if cur > len(src) || !eqLineWithOpt(src[cur-1], ln.Content, opt) {
					return "", 0, 0, errors.New("context mismatch (conflict)")
				}
				out = append(out, trimCR(src[cur-1]))
				cur++
			case Deleted:
				if cur > len(src) || !eqLineWithOpt(src[cur-1], ln.Content, opt) {
					return "", 0, 0, errors.New("delete target mismatch (conflict)")
				}
				cur++
				totalDel++
			case Added:
				out = append(out, ln.Content)
				totalAdd++
			}
		}
	}
	// copy the rest
	for cur <= len(src) {
		out = append(out, trimCR(src[cur-1]))
		cur++
	}
	// rejoin with newline if original had newline; if original ended with newline, keep it; else keep no extra
	res := strings.Join(out, "\n")
	if hasTrailingNewline(original) {
		res += "\n"
	}
	return res, totalAdd, totalDel, nil
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	// trim last \n for logical lines
	if s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n")
}

func hasTrailingNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}

// eqLine compares two logical lines tolerating CRLF differences.
func eqLine(a, b string) bool { return trimCR(a) == trimCR(b) }

func eqLineWithOpt(a, b string, opt ApplyOptions) bool {
	a = trimCR(a)
	b = trimCR(b)
	if opt.IgnoreWhitespace {
		a = strings.TrimSpace(a)
		b = strings.TrimSpace(b)
	}
	return a == b
}

func trimCR(x string) string {
	if len(x) > 0 && x[len(x)-1] == '\r' {
		return x[:len(x)-1]
	}
	return x
}
