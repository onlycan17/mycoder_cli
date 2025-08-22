package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type GoSymbol struct {
	Name      string
	Kind      string // func|method|type|var|const
	StartLine int
	EndLine   int
	Signature string
}

// ExtractGoSymbols parses Go source and returns exported symbols with line ranges.
func ExtractGoSymbols(src string) ([]GoSymbol, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "<memory>", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []GoSymbol
	add := func(name, kind string, n ast.Node, sig string) {
		if name == "" || !ast.IsExported(name) {
			return
		}
		pos := fset.Position(n.Pos()).Line
		end := fset.Position(n.End()).Line
		out = append(out, GoSymbol{Name: name, Kind: kind, StartLine: pos, EndLine: end, Signature: sig})
	}
	// map receiver type for method qualification (optional)
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl: // const|var|type
			k := ""
			switch x.Tok {
			case token.CONST:
				k = "const"
			case token.VAR:
				k = "var"
			case token.TYPE:
				k = "type"
			}
			if k == "" {
				return true
			}
			for _, spec := range x.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name.Name, k, s, s.Name.Name)
				case *ast.ValueSpec:
					for _, nm := range s.Names {
						add(nm.Name, k, s, nm.Name)
					}
				}
			}
			return false
		case *ast.FuncDecl:
			kind := "func"
			name := x.Name.Name
			sig := name
			if x.Recv != nil && len(x.Recv.List) > 0 {
				// method
				kind = "method"
				// receiver type (could be *T)
				var recvType string
				switch rt := x.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					if id, ok := rt.X.(*ast.Ident); ok {
						recvType = id.Name
					}
				case *ast.Ident:
					recvType = rt.Name
				default:
					recvType = "recv"
				}
				if recvType != "" {
					sig = recvType + "." + name
				}
			}
			// quick filter exported
			if ast.IsExported(name) {
				add(name, kind, x, sig)
			}
			return false
		}
		return true
	})
	// stable order by line, then name
	// simple insertion sort due to small typical counts
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 {
			if out[j-1].StartLine > out[j].StartLine || (out[j-1].StartLine == out[j].StartLine && strings.Compare(out[j-1].Name, out[j].Name) > 0) {
				out[j-1], out[j] = out[j], out[j-1]
				j--
			} else {
				break
			}
		}
	}
	return out, nil
}
