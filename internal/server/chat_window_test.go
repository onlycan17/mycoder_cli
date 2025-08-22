package server

import (
	"mycoder/internal/llm"
	"os"
	"testing"
)

func TestSlidingWindowKeepsSystemsAndRecent(t *testing.T) {
	os.Setenv("MYCODER_CHAT_MAX_CHARS", "20")
	defer os.Unsetenv("MYCODER_CHAT_MAX_CHARS")
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "rules"},
		{Role: llm.RoleUser, Content: "1234567890"},
		{Role: llm.RoleAssistant, Content: "abcde"},
		{Role: llm.RoleUser, Content: "xxxxx"},
	}
	out := slidingWindow(msgs)
	if len(out) == 0 || out[0].Role != llm.RoleSystem {
		t.Fatalf("expected system first, got %+v", out)
	}
	// budget 20 - len("rules")=5 => 15 chars for rest: should fit last two msgs ("abcde" + "xxxxx")
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (system + 2 recent), got %d", len(out))
	}
}
