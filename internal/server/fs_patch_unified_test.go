package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mycoder/internal/store"
)

func TestFSPatchUnifiedDryRun(t *testing.T) {
	st := store.New()
	api := NewAPI(st, nil)
	p := st.CreateProject("p", t.TempDir(), nil)
	mux := api.mux()
	diff := "" +
		"diff --git a/a.txt b/a.txt\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1,1 +1,2 @@\n" +
		"-hello\n" +
		"+hello world\n" +
		"+new\n"
	body, _ := json.Marshal(map[string]any{"projectID": p.ID, "diffText": diff, "dryRun": true})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/fs/patch/unified", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var res struct {
		Ok                 bool
		DryRun             bool
		TotalAdd, TotalDel float64
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if !res.Ok || !res.DryRun {
		t.Fatalf("unexpected: %+v", res)
	}
	if int(res.TotalAdd) != 2 || int(res.TotalDel) != 1 {
		t.Fatalf("totals: %+v", res)
	}
}
