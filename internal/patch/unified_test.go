package patch

import "testing"

const sample = `diff --git a/a.txt b/a.txt
index 83db48f..bf12a3a 100644
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,3 @@
 line1
-line2
+line2 modified
+line3
`

func TestParseUnifiedStats(t *testing.T) {
	files, err := ParseUnified(sample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%d", len(files))
	}
	if files[0].OldPath != "a.txt" || files[0].NewPath != "a.txt" {
		t.Fatalf("paths: %+v", files[0])
	}
	add, del := Stats(files)
	if add != 2 || del != 1 {
		t.Fatalf("stats add=%d del=%d", add, del)
	}
}

func TestGenerateUnifiedSimple(t *testing.T) {
	oldT := "a\nb\nc\n"
	newT := "a\nX\nc\nY\n"
	diff := GenerateUnified(oldT, newT, "t.txt", 2, true)
	if diff == "" {
		t.Fatalf("expected diff")
	}
	// parse back and check stats
	files, err := ParseUnified(diff)
	if err != nil || len(files) != 1 {
		t.Fatalf("parse back: %v", err)
	}
	add, del := Stats(files)
	if add != 2 || del != 1 {
		t.Fatalf("stats add=%d del=%d\n%s", add, del, diff)
	}
}
