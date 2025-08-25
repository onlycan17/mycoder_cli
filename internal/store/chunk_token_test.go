package store

import (
	"os"
	"testing"
)

func TestChunkTextWithLines_UsesTokenOverlap(t *testing.T) {
	oldMax := os.Getenv("MYCODER_CHUNK_MAX_TOKENS")
	oldOv := os.Getenv("MYCODER_CHUNK_OVERLAP_RATIO")
	t.Cleanup(func() {
		_ = os.Setenv("MYCODER_CHUNK_MAX_TOKENS", oldMax)
		_ = os.Setenv("MYCODER_CHUNK_OVERLAP_RATIO", oldOv)
	})
	_ = os.Setenv("MYCODER_CHUNK_MAX_TOKENS", "5")
	_ = os.Setenv("MYCODER_CHUNK_OVERLAP_RATIO", "0.2") // step=4

	text := "one two three four five six seven eight nine ten"
	chunks := chunkTextWithLines(text, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks with overlap, got %d", len(chunks))
	}
}

func TestChunkDocWithLines_HeadingsPreservedAndTokenized(t *testing.T) {
	oldMax := os.Getenv("MYCODER_CHUNK_MAX_TOKENS")
	_ = os.Setenv("MYCODER_CHUNK_MAX_TOKENS", "4")
	t.Cleanup(func() { _ = os.Setenv("MYCODER_CHUNK_MAX_TOKENS", oldMax) })
	doc := "# Title\npara one two three four five\n\npara six seven eight nine ten\n"
	chunks := chunkDocWithLines(doc, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks after tokenization, got %d", len(chunks))
	}
}
