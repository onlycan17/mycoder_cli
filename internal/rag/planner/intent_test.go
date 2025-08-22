package planner

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		q    string
		want Intent
	}{
		{"where is server.go?", IntentNavigate},
		{"이 함수 설명해줘", IntentExplain},
		{"refactor this module", IntentEdit},
		{"alternatives to pgvector", IntentResearch},
		{"unknown text", IntentUnknown},
	}
	for _, c := range cases {
		if got := Classify(c.q); got != c.want {
			t.Fatalf("Classify(%q)=%s want %s", c.q, got, c.want)
		}
	}
}

func TestRetrievalK(t *testing.T) {
	if k := RetrievalK(IntentExplain, 5); k < 7 {
		t.Fatalf("expected >=7, got %d", k)
	}
	if k := RetrievalK(IntentEdit, 5); k < 8 {
		t.Fatalf("expected >=8, got %d", k)
	}
	if k := RetrievalK(IntentResearch, 5); k < 10 {
		t.Fatalf("expected >=10, got %d", k)
	}
	if k := RetrievalK(IntentNavigate, 5); k != 5 {
		t.Fatalf("expected 5, got %d", k)
	}
}
