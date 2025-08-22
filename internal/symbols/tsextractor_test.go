package symbols

import "testing"

func TestExtractTSSymbols(t *testing.T) {
	src := `// sample TS
export function greet(name: string) { return 'hi' }
export class Box {}
export interface User { id: string }
export type ID = string
export const PI = 3.14
export var Global = 1
export let Flag = true
`
	syms, err := ExtractTSSymbols(src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	m := map[string]string{}
	for _, s := range syms {
		m[s.Name] = s.Kind
	}
	want := map[string]string{"greet": "function", "Box": "class", "User": "interface", "ID": "type", "PI": "const", "Global": "var", "Flag": "let"}
	for k, v := range want {
		if m[k] != v {
			t.Fatalf("missing %s kind=%s", k, m[k])
		}
	}
}
