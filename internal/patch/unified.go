package patch

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

type LineKind int

const (
	Context LineKind = iota
	Added
	Deleted
)

type UnifiedLine struct {
	Kind    LineKind
	Content string
}

type UnifiedHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []UnifiedLine
}

type UnifiedFile struct {
	OldPath string
	NewPath string
	Hunks   []UnifiedHunk
}

// ParseUnified parses a minimal unified diff text into files and hunks.
// Supported format: file headers (--- a/path, +++ b/path) and hunks starting with
// @@ -oldStart,oldCount +newStart,newCount @@.
func ParseUnified(diff string) ([]UnifiedFile, error) {
	var files []UnifiedFile
	var cur *UnifiedFile
	var scanner = bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "--- ") {
			// start new file; expect +++ next
			old := strings.TrimSpace(strings.TrimPrefix(line, "--- "))
			// read next line
			if !scanner.Scan() {
				return nil, fmt.Errorf("unexpected EOF after --- header")
			}
			pl := scanner.Text()
			if !strings.HasPrefix(pl, "+++ ") {
				return nil, fmt.Errorf("expected +++ after --- header")
			}
			newp := strings.TrimSpace(strings.TrimPrefix(pl, "+++ "))
			f := UnifiedFile{OldPath: stripPrefix(old), NewPath: stripPrefix(newp)}
			files = append(files, f)
			cur = &files[len(files)-1]
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			if cur == nil {
				return nil, fmt.Errorf("hunk without file header")
			}
			// @@ -l,s +l,s @@
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			// collect hunk lines until next header/file
			var lines []UnifiedLine
			for scanner.Scan() {
				l := scanner.Text()
				if strings.HasPrefix(l, "@@ ") || strings.HasPrefix(l, "--- ") {
					// push back by resetting scanner position is not trivial; instead, handle sentinel by setting line and looping.
					// We emulate by storing in a temp and using a small trick: reuse current line in next outer loop by prefacing with marker.
					// Simpler: we update scanner's current line pointer by keeping a carry value.
					// To keep parser simple, we keep this hunk and process this new header in outer loop using a small recursion.
					// Implement by finalizing current hunk and then recursively parsing the rest.
					// Since Scanner doesn't support unread, we construct the remainder and parse recursively.
					// First, append current hunk and then parse remainder with another call.
					cur.Hunks = append(cur.Hunks, UnifiedHunk{OldStart: h.OldStart, OldCount: h.OldCount, NewStart: h.NewStart, NewCount: h.NewCount, Lines: lines})
					// Build remainder string
					rest := l + "\n" + readRest(scanner)
					more, err := ParseUnified("--- " + cur.OldPath + "\n+++ " + cur.NewPath + "\n" + rest)
					if err != nil {
						return nil, err
					}
					// merge hunks from more[0] into current
					if len(more) > 0 {
						cur.Hunks = append(cur.Hunks, more[0].Hunks...)
						// and append any subsequent files
						if len(more) > 1 {
							files = append(files, more[1:]...)
						}
					}
					return files, nil
				}
				if len(l) == 0 {
					lines = append(lines, UnifiedLine{Kind: Context, Content: ""})
					continue
				}
				switch l[0] {
				case ' ':
					lines = append(lines, UnifiedLine{Kind: Context, Content: l[1:]})
				case '+':
					lines = append(lines, UnifiedLine{Kind: Added, Content: l[1:]})
				case '-':
					lines = append(lines, UnifiedLine{Kind: Deleted, Content: l[1:]})
				default:
					// treat as context for robustness
					lines = append(lines, UnifiedLine{Kind: Context, Content: l})
				}
			}
			cur.Hunks = append(cur.Hunks, UnifiedHunk{OldStart: h.OldStart, OldCount: h.OldCount, NewStart: h.NewStart, NewCount: h.NewCount, Lines: lines})
			continue
		}
		// ignore other lines (e.g., diff --git)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func stripPrefix(s string) string {
	// common prefixes: a/ b/
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

type hinfo struct{ OldStart, OldCount, NewStart, NewCount int }

func parseHunkHeader(line string) (hinfo, error) {
	// format: @@ -oldStart,oldCount +newStart,newCount @@
	// counts may omit ,count meaning 1
	// strip @@ and split
	// example: @@ -10,2 +10,3 @@
	parts := strings.Split(line, " ")
	if len(parts) < 4 {
		return hinfo{}, fmt.Errorf("invalid hunk header: %s", line)
	}
	parseRange := func(s string) (int, int, error) {
		if len(s) == 0 {
			return 0, 0, fmt.Errorf("empty range")
		}
		if s[0] == '-' || s[0] == '+' {
			s = s[1:]
		}
		n := 1
		if i := strings.IndexByte(s, ','); i >= 0 {
			a := s[:i]
			b := s[i+1:]
			v1, err := strconv.Atoi(a)
			if err != nil {
				return 0, 0, err
			}
			v2, err := strconv.Atoi(b)
			if err != nil {
				return 0, 0, err
			}
			return v1, v2, nil
		}
		v1, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, err
		}
		return v1, n, nil
	}
	oldStart, oldCount, err := parseRange(parts[1])
	if err != nil {
		return hinfo{}, err
	}
	newStart, newCount, err := parseRange(parts[2])
	if err != nil {
		return hinfo{}, err
	}
	return hinfo{OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount}, nil
}

func readRest(s *bufio.Scanner) string {
	var b strings.Builder
	for s.Scan() {
		b.WriteString(s.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// Stats returns added/deleted line counts per file.
func Stats(files []UnifiedFile) (add, del int) {
	for _, f := range files {
		for _, h := range f.Hunks {
			for _, ln := range h.Lines {
				switch ln.Kind {
				case Added:
					add++
				case Deleted:
					del++
				}
			}
		}
	}
	return
}
