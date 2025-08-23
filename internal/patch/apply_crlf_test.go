package patch

import "testing"

func TestApplyCRLFTolerant(t *testing.T) {
	orig := "line1\r\nline2\r\n"
	hunks := []UnifiedHunk{{OldStart: 1, OldCount: 2, NewStart: 1, NewCount: 2, Lines: []UnifiedLine{
		{Kind: Context, Content: "line1"},
		{Kind: Deleted, Content: "line2"},
		{Kind: Added, Content: "line2 modified"},
	}}}
	out, add, del, err := ApplyToContent(orig, hunks)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if add != 1 || del != 1 {
		t.Fatalf("stats add=%d del=%d", add, del)
	}
	if out != "line1\nline2 modified\n" {
		t.Fatalf("out=%q", out)
	}
}
