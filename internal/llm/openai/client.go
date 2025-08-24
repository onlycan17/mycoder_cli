package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"mycoder/internal/llm"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	minGap  time.Duration
	lastReq time.Time
}

func NewFromEnv() *Client {
	base := os.Getenv("MYCODER_OPENAI_BASE_URL")
	if base == "" {
		// 기본을 LM Studio 외부 접근 가능한 호스트로 설정
		base = "http://210.126.109.57:3620/v1"
	}
	key := os.Getenv("MYCODER_OPENAI_API_KEY")
	gap := time.Duration(0)
	if ms := os.Getenv("MYCODER_LLM_MIN_INTERVAL_MS"); ms != "" {
		if v, err := strconv.Atoi(ms); err == nil && v > 0 {
			gap = time.Duration(v) * time.Millisecond
		}
	}
	return &Client{baseURL: strings.TrimRight(base, "/"), apiKey: key, http: &http.Client{Timeout: 60 * time.Second}, minGap: gap}
}

type chatStream struct {
	body io.ReadCloser
	r    *bufio.Reader
}

func (s *chatStream) Recv() (string, bool, error) {
	line, err := s.r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", true, nil
		}
		return "", true, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return "", false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "[DONE]" {
		return "", true, nil
	}
	var evt struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		return "", false, nil
	}
	if len(evt.Choices) > 0 {
		return evt.Choices[0].Delta.Content, false, nil
	}
	return "", false, nil
}

func (s *chatStream) Close() error { return s.body.Close() }

// Chat implements llm.ChatProvider using OpenAI-compatible API.
func (c *Client) Chat(ctx context.Context, model string, messages []llm.Message, stream bool, temperature float32) (llm.ChatStream, error) {
	if model == "" {
		model = os.Getenv("MYCODER_CHAT_MODEL")
		if model == "" {
			// 기본 모델 설정
			model = "qwen2.5-7b-instruct-1m"
		}
	}
	reqBody := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": temperature,
		"stream":      stream,
	}
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("chat http %d: %s", resp.StatusCode, string(data))
	}
	if stream {
		return &chatStream{body: resp.Body, r: bufio.NewReader(resp.Body)}, nil
	}
	// non-streaming: read once and return as a single chunk then done
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()
	content := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
	}
	return &staticStream{s: content}, nil
}

type staticStream struct{ s string }

func (s *staticStream) Recv() (string, bool, error) {
	if s.s == "" {
		return "", true, nil
	}
	v := s.s
	s.s = ""
	return v, false, nil
}
func (s *staticStream) Close() error { return nil }

// Embeddings implements llm.Embedder using OpenAI-compatible API.
func (c *Client) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if model == "" {
		model = os.Getenv("MYCODER_EMBEDDING_MODEL")
		if model == "" {
			// 기본 임베딩 모델 설정
			model = "text-embedding-nomic-embed-text-v1.5"
		}
	}
	reqBody := map[string]any{
		"model": model,
		"input": inputs,
	}
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings http %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := make([][]float32, 0, len(out.Data))
	for _, d := range out.Data {
		res = append(res, d.Embedding)
	}
	return res, nil
}

// ListModels fetches available model IDs via GET /models
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("models http %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Completion calls POST /completions (non-chat) and adapts to ChatStream interface.
func (c *Client) Completion(ctx context.Context, model, prompt string, stream bool, temperature float32) (llm.ChatStream, error) {
	if model == "" {
		model = os.Getenv("MYCODER_CHAT_MODEL")
	}
	body := map[string]any{"model": model, "prompt": prompt, "temperature": temperature, "stream": stream}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("comp http %d: %s", resp.StatusCode, string(data))
	}
	if stream {
		return &chatStream{body: resp.Body, r: bufio.NewReader(resp.Body)}, nil
	}
	var out struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()
	s := ""
	if len(out.Choices) > 0 {
		s = out.Choices[0].Text
	}
	return &staticStream{s: s}, nil
}

// do performs the HTTP request with optional min interval and retries on 429/5xx.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.minGap > 0 {
		since := time.Since(c.lastReq)
		if since < c.minGap {
			time.Sleep(c.minGap - since)
		}
	}
	var resp *http.Response
	var err error
	backoff := 200 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.http.Do(req)
		c.lastReq = time.Now()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 429 && resp.StatusCode/100 != 5 {
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		time.Sleep(backoff + time.Duration(attempt)*100*time.Millisecond)
	}
	return c.http.Do(req)
}
