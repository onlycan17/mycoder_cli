package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestMCPToolsIncludesSchema(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/mcp/tools", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res struct {
		Tools []struct {
			Name         string   `json:"name"`
			Params       []string `json:"params"`
			ParamsSchema []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
			} `json:"paramsSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(res.Tools) == 0 || res.Tools[0].Name == "" {
		t.Fatalf("expected at least one tool with name")
	}
	// echo must have required text param
	var echoFound bool
	for _, ttool := range res.Tools {
		if ttool.Name == "echo" {
			echoFound = true
			if len(ttool.ParamsSchema) == 0 || ttool.ParamsSchema[0].Name != "text" || ttool.ParamsSchema[0].Type != "string" || !ttool.ParamsSchema[0].Required {
				t.Fatalf("echo schema missing or invalid: %+v", ttool.ParamsSchema)
			}
		}
	}
	if !echoFound {
		t.Fatalf("echo tool not found")
	}
}

func TestMCPCallValidation(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	// missing text
	body, _ := json.Marshal(map[string]any{"name": "echo", "params": map[string]any{}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("expected ok=false for missing param")
	}
	// correct call
	body2, _ := json.Marshal(map[string]any{"name": "echo", "params": map[string]any{"text": "hi"}})
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body2)))
	if rr2.Code != http.StatusOK {
		t.Fatalf("code2=%d", rr2.Code)
	}
	var res2 map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &res2)
	if ok, _ := res2["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got: %v", res2)
	}
}

func TestMCPAllowlistAndScope(t *testing.T) {
	// allow only echo
	t.Setenv("MYCODER_MCP_ALLOWED_TOOLS", "echo")
	t.Setenv("MYCODER_MCP_REQUIRED_SCOPE", "mcp:call")
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	// tools list should only include echo
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/mcp/tools", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	var list struct{ Tools []struct{ Name string } }
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Fatalf("allowlist filter failed: %+v", list.Tools)
	}
	// call without scope -> forbidden
	body, _ := json.Marshal(map[string]any{"name": "echo", "params": map[string]any{"text": "hi"}})
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body)))
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without scope, got %d", rr2.Code)
	}
	// call with scope -> ok
	req := httptest.NewRequest(http.MethodPost, "/mcp/call", bytes.NewReader(body))
	req.Header.Set("X-MYCODER-Scope", "mcp:call:echo")
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200 with scope, got %d", rr3.Code)
	}
}
