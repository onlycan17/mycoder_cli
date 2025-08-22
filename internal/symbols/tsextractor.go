package symbols

import (
	"bufio"
	"regexp"
	"strings"
)

type TSSymbol struct {
	Name      string
	Kind      string // function|class|interface|type|const|var|let
	StartLine int
	EndLine   int
	Signature string
}

var (
	reFunc      = regexp.MustCompile(`^\s*export\s+(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reClass     = regexp.MustCompile(`^\s*export\s+class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reInterface = regexp.MustCompile(`^\s*export\s+interface\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reType      = regexp.MustCompile(`^\s*export\s+type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reConst     = regexp.MustCompile(`^\s*export\s+const\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reVar       = regexp.MustCompile(`^\s*export\s+var\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reLet       = regexp.MustCompile(`^\s*export\s+let\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
)

// ExtractTSSymbols scans TypeScript/TSX source text line-by-line and extracts
// exported top-level symbols with rough line numbers.
func ExtractTSSymbols(src string) ([]TSSymbol, error) {
	var out []TSSymbol
	rd := bufio.NewScanner(strings.NewReader(src))
	line := 0
	for rd.Scan() {
		line++
		s := rd.Text()
		// skip line comments
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if m := reFunc.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "function", StartLine: line, EndLine: line, Signature: m[1] + "()"})
			continue
		}
		if m := reClass.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "class", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
		if m := reInterface.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "interface", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
		if m := reType.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "type", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
		if m := reConst.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "const", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
		if m := reVar.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "var", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
		if m := reLet.FindStringSubmatch(s); len(m) == 2 {
			out = append(out, TSSymbol{Name: m[1], Kind: "let", StartLine: line, EndLine: line, Signature: m[1]})
			continue
		}
	}
	return out, nil
}
