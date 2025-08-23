package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestStandardErrorFormat_SearchMissingQ(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/search", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rr.Code)
	}
	var e struct {
		Error, Message string
		Code           int
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("json: %v", err)
	}
	if e.Error != "invalid_request" || e.Message == "" || e.Code != 400 {
		t.Fatalf("unexpected error body: %+v", e)
	}
}

func TestStandardErrorFormat_FSWriteInvalid(t *testing.T) {
	api := NewAPI(store.New(), nil)
	mux := api.mux()
	body := []byte(`{"projectID":"","path":""}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/write", bytes.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rr.Code)
	}
	var e struct {
		Error, Message string
		Code           int
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &e)
	if e.Error != "invalid_request" || e.Code != 400 {
		t.Fatalf("unexpected error body: %+v", e)
	}
}
