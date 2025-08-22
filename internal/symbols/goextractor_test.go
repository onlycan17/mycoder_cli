package symbols

import "testing"

func TestExtractGoSymbols(t *testing.T) {
	src := `package p
type Foo struct{X int}
func (f *Foo) Bar() {}
func Util(){}
var ExportedVar = 1
const ExportedConst = 2
func unexported(){}
`
	syms, err := ExtractGoSymbols(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	names := make(map[string]string)
	for _, s := range syms {
		names[s.Name] = s.Kind
	}
	want := map[string]string{"Foo": "type", "Bar": "method", "Util": "func", "ExportedVar": "var", "ExportedConst": "const"}
	for k, v := range want {
		if names[k] != v {
			t.Fatalf("missing or wrong kind for %s: %s", k, names[k])
		}
	}
	if _, ok := names["unexported"]; ok {
		t.Fatalf("should not include unexported")
	}
}
