package llm

import (
	"net/http"
	"os"
	"testing"
	"time"
)

// Opt-in LM Studio smoke test: set MYCODER_LMSTUDIO_SMOKE=1 and MYCODER_OPENAI_BASE_URL=http://localhost:1234/v1
func TestLMStudioSmoke_Models(t *testing.T) {
	if os.Getenv("MYCODER_LMSTUDIO_SMOKE") != "1" {
		t.Skip("LM Studio smoke test skipped (set MYCODER_LMSTUDIO_SMOKE=1 to enable)")
	}
	base := os.Getenv("MYCODER_OPENAI_BASE_URL")
	if base == "" {
		t.Skip("MYCODER_OPENAI_BASE_URL not set")
	}
	url := base
	if url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	url += "/models"
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if key := os.Getenv("MYCODER_OPENAI_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}
