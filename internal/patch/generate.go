package patch

import (
	"fmt"
	"strings"
)

// GenerateUnified produces a minimal unified diff for a single file.
// path is used in headers as both old/new (a/path, b/path). Context controls the
// number of context lines around changes.
func GenerateUnified(oldText, newText, path string, context int, ignoreCRLF bool) string {
	// normalize comparison lines
	toLines := func(s string) []string {
		if s == "" {
			return []string{}
		}
		// keep logical lines without trailing final newline
		if s[len(s)-1] == '\n' {
			s = s[:len(s)-1]
		}
		ls := strings.Split(s, "\n")
		if ignoreCRLF {
			for i := range ls {
				ls[i] = trimCR(ls[i])
			}
		}
		return ls
	}
	a := toLines(oldText)
	b := toLines(newText)
	// LCS table
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	// backtrack to get edit script
	type edit struct {
		kind byte
		s    string
	} // ' ' context, '-' del, '+' add
	i, j := 0, 0
	var script []edit
	for i < n && j < m {
		if a[i] == b[j] {
			script = append(script, edit{' ', a[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			script = append(script, edit{'-', a[i]})
			i++
		} else {
			script = append(script, edit{'+', b[j]})
			j++
		}
	}
	for i < n {
		script = append(script, edit{'-', a[i]})
		i++
	}
	for j < m {
		script = append(script, edit{'+', b[j]})
		j++
	}
	// group into hunks with context
	if context < 0 {
		context = 3
	}
	type hunk struct {
		startA, countA, startB, countB int
		lines                          []edit
	}
	var hunks []hunk
	// walk script to find change blocks
	idx := 0
	for idx < len(script) {
		// skip context until change
		for idx < len(script) && script[idx].kind == ' ' {
			idx++
		}
		if idx >= len(script) {
			break
		}
		// find change region [L..R)
		L := idx
		R := idx
		for R < len(script) {
			// extend until we see context > context
			// track last change position
			if script[R].kind != ' ' {
				R++
				continue
			}
			// count following context
			k := 0
			for k < context && R+k < len(script) && script[R+k].kind == ' ' {
				k++
			}
			if k < context {
				R += k
				continue
			} // not enough, include and continue
			// enough context to end hunk
			R += k
			break
		}
		if R > len(script) {
			R = len(script)
		}
		// compute header positions
		// aStart is count of non-added before L among script
		aStart, bStart := 1, 1
		for t := 0; t < L; t++ {
			if script[t].kind != '+' {
				aStart++
			}
			if script[t].kind != '-' {
				bStart++
			}
		}
		countA, countB := 0, 0
		lines := make([]edit, 0, R-L+2*context)
		// prepend leading context up to 'context'
		cpre := 0
		for t := L - 1; t >= 0 && cpre < context; t-- {
			if script[t].kind == ' ' {
				lines = append([]edit{script[t]}, lines...)
				cpre++
			} else {
				break
			}
		}
		// main region
		for t := L; t < R && t < len(script); t++ {
			lines = append(lines, script[t])
		}
		// append trailing context up to context lines after R
		cpost := 0
		for t := R; t < len(script) && cpost < context && script[t].kind == ' '; t++ {
			lines = append(lines, script[t])
			cpost++
		}
		for _, e := range lines {
			if e.kind != '+' {
				countA++
			}
			if e.kind != '-' {
				countB++
			}
		}
		hunks = append(hunks, hunk{startA: aStart, countA: countA, startB: bStart, countB: countB, lines: lines})
		idx = R
	}
	// if no changes
	if len(hunks) == 0 {
		return ""
	}
	var bld strings.Builder
	fmt.Fprintf(&bld, "--- a/%s\n", path)
	fmt.Fprintf(&bld, "+++ b/%s\n", path)
	for _, h := range hunks {
		fmt.Fprintf(&bld, "@@ -%d,%d +%d,%d @@\n", h.startA, h.countA, h.startB, h.countB)
		for _, e := range h.lines {
			bld.WriteByte(e.kind)
			bld.WriteString(e.s)
			bld.WriteByte('\n')
		}
	}
	return bld.String()
}
