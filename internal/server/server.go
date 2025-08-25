package server

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"io"
	"math/rand"
	"mycoder/internal/patch"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

import (
	"encoding/json"
	"mycoder/internal/indexer"
	"mycoder/internal/indexer/embedpipe"
	"mycoder/internal/llm"
	oai "mycoder/internal/llm/openai"
	mylog "mycoder/internal/log"
	"mycoder/internal/models"
	"mycoder/internal/rag/planner"
	"mycoder/internal/rag/retriever"
	"mycoder/internal/store"
	"mycoder/internal/vectorstore"
	"mycoder/internal/version"
	"strconv"
)

// HooksResult is the structured summary per hook target.
type HooksResult struct {
	Ok         bool   `json:"ok"`
	Output     string `json:"output"`
	Suggestion string `json:"suggestion,omitempty"`
	Reason     string `json:"reason,omitempty"`
	DurationMs int    `json:"durationMs"`
	Lines      int    `json:"lines"`
	Bytes      int    `json:"bytes"`
}

type Store interface {
	CreateProject(name, root string, ignore []string) *models.Project
	ListProjects() []*models.Project
	GetProject(id string) (*models.Project, bool)
	// jobs
	CreateIndexJob(projectID string, mode models.IndexMode) (*models.IndexJob, error)
	SetJobStatus(id string, st models.IndexJobStatus, stats map[string]int) (*models.IndexJob, error)
	GetJob(id string) (*models.IndexJob, bool)
	// docs/search
	AddDocument(projectID, path, content string) *models.Document
	Search(projectID, query string, k int) []models.SearchResult
	// metrics
	Stats() map[string]int
	// knowledge
	AddKnowledge(projectID, sourceType, pathOrURL, title, text string, trust float64, pinned bool) (*models.Knowledge, error)
	ListKnowledge(projectID string, minScore float64) ([]*models.Knowledge, error)
	VetKnowledge(projectID string) (int, error)
	PromoteKnowledge(projectID, title, text, pathOrURL, commitSHA, filesCSV, symbolsCSV string, pin bool) (*models.Knowledge, error)
	ReverifyKnowledge(projectID string) (int, error)
	GCKnowledge(projectID string, minScore float64) (int, error)
	ApproveKnowledge(projectID string, ids []string, pin bool, minTrust float64) (int, error)
}

type IncrementalStore interface {
	UpsertDocument(projectID, path, content, sha, lang, mtime string) *models.Document
	PruneDocuments(projectID string, present []string) error
}

type API struct {
	store Store
	llm   llm.ChatProvider
	emb   llm.Embedder
	vs    vectorstore.VectorStore
}

func NewAPI(s Store, p llm.ChatProvider) *API {
	lg := mylog.New()
	a := &API{store: s, llm: p}
	if e, ok := any(p).(llm.Embedder); ok {
		a.emb = e
		lg.Info("embeddings.provider", "status", "found")
	} else {
		lg.Info("embeddings.provider", "status", "not_found")
	}
	// Prefer SQLite-backed VectorStore when available; fallback to env provider.
	if ss, ok := s.(*store.SQLiteStore); ok {
		a.vs = vectorstore.NewSQLite(ss.DB())
	} else {
		a.vs = vectorstore.NewFromEnv()
	}
	if a.emb != nil && os.Getenv("MYCODER_EMBED_CACHE_DISABLE") != "1" {
		a.emb = newCachingEmbedder(a.emb)
		lg.Info("embeddings.cache", "status", "enabled")
	}
	// embedding availability check and env opt-out
	if os.Getenv("MYCODER_DISABLE_EMBEDDINGS") == "1" {
		lg.Info("embeddings.disabled", "reason", "env_var_set")
		a.emb = nil
	} else if a.emb != nil {
		// quick health check: tiny embedding with reasonable timeout for remote servers
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		embModel := os.Getenv("MYCODER_EMBEDDING_MODEL")
		lg.Info("embeddings.health_check", "model", embModel)
		if _, err := a.emb.Embeddings(ctx, embModel, []string{"ping"}); err != nil {
			lg.Warn("embeddings.disabled", "reason", err.Error(), "model", embModel)
			a.emb = nil
		} else {
			lg.Info("embeddings.enabled", "model", embModel)
		}
	}
	return a
}

// capBuffer captures writes up to a fixed limit and marks truncation beyond it.
type capBuffer struct {
	b         []byte
	n         int
	cap       int
	truncated bool
	lines     int
}

func newCapBuffer(limit int) *capBuffer { return &capBuffer{b: make([]byte, 0, limit), cap: limit} }

func (c *capBuffer) Write(p []byte) (int, error) {
	remain := c.cap - c.n
	if remain > 0 {
		write := p
		if len(p) > remain {
			write = p[:remain]
			c.truncated = true
		}
		// count newlines in the portion we keep
		for i := 0; i < len(write); i++ {
			if write[i] == '\n' {
				c.lines++
			}
		}
		c.b = append(c.b, write...)
		c.n += len(write)
	} else {
		c.truncated = true
	}
	return len(p), nil
}

// Shell policy (allow/deny regex)
var (
	shellPolicyOnce sync.Once
	allowRe         *regexp.Regexp
	denyRe          *regexp.Regexp
)

func loadShellPolicy() {
	shellPolicyOnce.Do(func() {
		if v := os.Getenv("MYCODER_SHELL_ALLOW_REGEX"); v != "" {
			allowRe, _ = regexp.Compile(v)
		}
		if v := os.Getenv("MYCODER_SHELL_DENY_REGEX"); v != "" {
			denyRe, _ = regexp.Compile(v)
		}
	})
}

func shellAllowed(cmdline string) (bool, string) {
	loadShellPolicy()
	if denyRe != nil && denyRe.MatchString(cmdline) {
		return false, "command denied by policy"
	}
	if allowRe != nil && !allowRe.MatchString(cmdline) {
		return false, "command not allowed by policy"
	}
	return true, ""
}

// FS policy (allow/deny regex) on relative path
var (
	fsPolicyOnce sync.Once
	fsAllowRe    *regexp.Regexp
	fsDenyRe     *regexp.Regexp
)

func loadFSPolicy() {
	fsPolicyOnce.Do(func() {
		if v := os.Getenv("MYCODER_FS_ALLOW_REGEX"); v != "" {
			fsAllowRe, _ = regexp.Compile(v)
		}
		if v := os.Getenv("MYCODER_FS_DENY_REGEX"); v != "" {
			fsDenyRe, _ = regexp.Compile(v)
		}
	})
}

func fsAllowed(rel string) (bool, string) {
	loadFSPolicy()
	// Late-binding for tests/env changes: re-read if unset
	if fsAllowRe == nil {
		if v := os.Getenv("MYCODER_FS_ALLOW_REGEX"); v != "" {
			fsAllowRe, _ = regexp.Compile(v)
		}
	}
	if fsDenyRe == nil {
		if v := os.Getenv("MYCODER_FS_DENY_REGEX"); v != "" {
			fsDenyRe, _ = regexp.Compile(v)
		}
	}
	if fsDenyRe != nil && fsDenyRe.MatchString(rel) {
		return false, "fs path denied by policy"
	}
	if fsAllowRe != nil && !fsAllowRe.MatchString(rel) {
		return false, "fs path not allowed by policy"
	}
	return true, ""
}

// lightweight in-process metrics collector
type metricsCollector struct {
	mu sync.Mutex
	// counters keyed by method|path|status
	reqTotal map[string]int
	// duration sum/count keyed by method|path
	durSum   map[string]float64
	durCount map[string]int
	// chat-related
	chatRequests int
	chatTokens   int
	// embedding cache
	embedCacheHits   int
	embedCacheMisses int
	embedCacheEvict  int
}

// Authorization: optional token via env MYCODER_API_TOKEN.
// Accepts Authorization: Bearer <token> or query param ?token=...
func authorize(w http.ResponseWriter, r *http.Request) bool {
	tok := os.Getenv("MYCODER_API_TOKEN")
	if tok == "" {
		return true
	}
	hdr := r.Header.Get("Authorization")
	if strings.HasPrefix(hdr, "Bearer ") && strings.TrimSpace(hdr[len("Bearer "):]) == tok {
		return true
	}
	if r.URL.Query().Get("token") == tok {
		return true
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid token")
	return false
}

func isReadOnly() bool { return os.Getenv("MYCODER_READONLY") == "1" }

func newMetrics() *metricsCollector {
	return &metricsCollector{
		reqTotal: make(map[string]int),
		durSum:   make(map[string]float64),
		durCount: make(map[string]int),
	}
}

var metrics = newMetrics()

// sampling for metrics recording (0..1)
var (
	metricsSampleRate = 1.0
	samplerOnce       sync.Once
)

func shouldSample() bool {
	samplerOnce.Do(func() {
		if v := os.Getenv("MYCODER_METRICS_SAMPLE_RATE"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
				metricsSampleRate = f
			}
		}
		rand.Seed(time.Now().UnixNano())
	})
	if metricsSampleRate >= 1 {
		return true
	}
	return rand.Float64() < metricsSampleRate
}

// normalizePath collapses variable path segments for metrics labels
func normalizePath(p string) string {
	if strings.HasPrefix(p, "/index/jobs/") {
		return "/index/jobs/:id"
	}
	return p
}

func (a *API) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/projects", a.handleProjects)
	mux.HandleFunc("/index/run", a.handleIndexRun)
	mux.HandleFunc("/index/run/stream", a.handleIndexRunStream)
	mux.HandleFunc("/index/jobs/", a.handleIndexJob)
	mux.HandleFunc("/search", a.handleSearch)
	mux.HandleFunc("/metrics", a.handleMetrics)
	mux.HandleFunc("/fs/read", a.handleFSRead)
	mux.HandleFunc("/fs/write", a.handleFSWrite)
	mux.HandleFunc("/fs/patch", a.handleFSPatch)
	mux.HandleFunc("/fs/patch/unified", a.handleFSPatchUnified)
	mux.HandleFunc("/fs/patch/unified/rollback", a.handleFSPatchUnifiedRollback)
	mux.HandleFunc("/fs/diff", a.handleFSDiff)
	mux.HandleFunc("/fs/delete", a.handleFSDelete)
	mux.HandleFunc("/shell/exec", a.handleShellExec)
	mux.HandleFunc("/shell/exec/stream", a.handleShellExecStream)
	mux.HandleFunc("/chat", a.handleChat)
	// knowledge curation
	mux.HandleFunc("/knowledge", a.handleKnowledge)
	mux.HandleFunc("/knowledge/vet", a.handleKnowledgeVet)
	mux.HandleFunc("/knowledge/promote", a.handleKnowledgePromote)
	mux.HandleFunc("/knowledge/approve", a.handleKnowledgeApprove)
	mux.HandleFunc("/knowledge/reverify", a.handleKnowledgeReverify)
	mux.HandleFunc("/knowledge/pending", a.handleKnowledgePending)
	mux.HandleFunc("/knowledge/gc", a.handleKnowledgeGC)
	mux.HandleFunc("/knowledge/promote/auto", a.handleKnowledgePromoteAuto)
	// tools/hooks
	mux.HandleFunc("/tools/hooks", a.handleToolsHooks)
	// mcp tools
	mux.HandleFunc("/mcp/tools", a.handleMCPTools)
	mux.HandleFunc("/mcp/call", a.handleMCPCall)
	// web enrichment (optional)
	mux.HandleFunc("/web/search", a.handleWebSearch)
	mux.HandleFunc("/web/ingest", a.handleWebIngest)
	return mux
}

// Run starts an HTTP server with a minimal health endpoint.
func Run(addr string) error {
	var st Store
	if path := os.Getenv("MYCODER_SQLITE_PATH"); path != "" {
		if sdb, err := store.NewSQLite(path); err == nil {
			st = sdb
		} else {
			fmt.Fprintf(os.Stderr, "sqlite init failed: %v, falling back to memory\n", err)
			st = store.New()
		}
	} else {
		st = store.New()
	}
	// select LLM provider
	var prov llm.ChatProvider
	switch strings.ToLower(os.Getenv("MYCODER_LLM_PROVIDER")) {
	case "", "openai":
		prov = oai.NewFromEnv()
	default:
		prov = oai.NewFromEnv()
	}
	api := NewAPI(st, prov)
	mux := api.mux()
	// optional background curator (decay/reverify/gc)
	if os.Getenv("MYCODER_CURATOR_DISABLE") == "" {
		interval := 10 * time.Minute
		if v := os.Getenv("MYCODER_CURATOR_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				interval = d
			}
		}
		minTrust := 0.4
		if v := os.Getenv("MYCODER_KNOWLEDGE_MIN_TRUST"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				minTrust = f
			}
		}
		decayRate := 0.01
		if v := os.Getenv("MYCODER_KNOWLEDGE_DECAY_RATE"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				decayRate = f
			}
		}
		decayAfterDays := 30
		if v := os.Getenv("MYCODER_KNOWLEDGE_DECAY_AFTER_DAYS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				decayAfterDays = n
			}
		}
		go func() {
			t := time.NewTicker(interval)
			defer t.Stop()
			for range t.C {
				for _, p := range st.ListProjects() {
					if decayRate > 0 {
						if ss, ok := st.(*store.SQLiteStore); ok {
							_, _ = ss.DecayKnowledge(p.ID, decayRate, decayAfterDays)
						}
					}
					_, _ = st.ReverifyKnowledge(p.ID)
					_, _ = st.GCKnowledge(p.ID, minTrust)
					// TTL-based GC for web knowledge via tags.ttlUntil
					if ss, ok := st.(*store.SQLiteStore); ok {
						_, _ = ss.GCKnowledgeTTL(p.ID)
					}
				}
			}
		}()
	}

	// optional background conversation cleanup (TTL/pin retention)
	// Controls:
	// - MYCODER_CONV_CLEAN_DISABLE: if set, disables cleaner
	// - MYCODER_CONV_TTL_DAYS: TTL in days for non-pinned conversations (default 30)
	// - MYCODER_CONV_CLEAN_INTERVAL: interval for cleanup loop (default 24h)
	if os.Getenv("MYCODER_CONV_CLEAN_DISABLE") == "" {
		ttlDays := 30
		if v := os.Getenv("MYCODER_CONV_TTL_DAYS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				ttlDays = n
			}
		}
		interval := 24 * time.Hour
		if v := os.Getenv("MYCODER_CONV_CLEAN_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				interval = d
			}
		}
		if ss, ok := st.(*store.SQLiteStore); ok && ttlDays > 0 {
			go func() {
				t := time.NewTicker(interval)
				defer t.Stop()
				for range t.C {
					_, _ = ss.CleanupConversations(ttlDays)
				}
			}()
		}
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           logMiddleware(rateLimitMiddleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		errs <- srv.ListenAndServe()
	}()

	// graceful shutdown on SIGINT/SIGTERM
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigc:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return fmt.Errorf("shutdown by signal: %v", sig)
	case err := <-errs:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	nbytes int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	n, err := sr.ResponseWriter.Write(b)
	sr.nbytes += n
	return n, err
}

// newRequestID returns a short, unique request identifier.
func newRequestID() string {
	var b [12]byte
	if _, err := crand.Read(b[:]); err != nil {
		// fallback: monotonic timestamp string
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 24)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

// clientIP extracts the best-effort client IP from headers or RemoteAddr.
func clientIP(r *http.Request) string {
	// X-Forwarded-For may contain a comma-separated list; take the first
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}
	if rip := strings.TrimSpace(r.Header.Get("X-Real-IP")); rip != "" {
		return rip
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

// rateLimiter provides simple token-bucket rate limiting by key.
type rateLimiter struct {
	mu      sync.Mutex
	rps     float64
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rps float64) *rateLimiter {
	return &rateLimiter{rps: rps, buckets: make(map[string]*bucket)}
}

// allow reports whether a request with key is allowed now and, if not, the seconds until next token.
func (rl *rateLimiter) allow(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.rps <= 0 {
		return true, 0
	}
	b := rl.buckets[key]
	now := time.Now()
	if b == nil {
		b = &bucket{tokens: rl.rps, last: now}
		rl.buckets[key] = b
	}
	// refill tokens
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = minFloat(b.tokens+elapsed*rl.rps, rl.rps)
	b.last = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true, 0
	}
	// compute wait time for next whole token
	need := 1 - b.tokens
	wait := int(need/rl.rps + 0.999) // ceil to whole seconds
	if wait < 1 {
		wait = 1
	}
	return false, wait
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// rateLimitMiddleware enforces basic RPS limits across global, path, and client scopes.
func rateLimitMiddleware(next http.Handler) http.Handler {
	// read env once on first use
	var once sync.Once
	var gLimiter, pLimiter, iLimiter *rateLimiter
	init := func() {
		// fallbacks to MYCODER_RATE_LIMIT_RPS when specific not set
		base := parseFloatEnv("MYCODER_RATE_LIMIT_RPS")
		g := parseFloatEnv("MYCODER_RATE_LIMIT_GLOBAL_RPS")
		p := parseFloatEnv("MYCODER_RATE_LIMIT_PATH_RPS")
		i := parseFloatEnv("MYCODER_RATE_LIMIT_IP_RPS")
		if g == -1 {
			g = base
		}
		if p == -1 {
			p = base
		}
		if i == -1 {
			i = base
		}
		gLimiter = newRateLimiter(g)
		pLimiter = newRateLimiter(p)
		iLimiter = newRateLimiter(i)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(init)
		if (gLimiter == nil || gLimiter.rps <= 0) && (pLimiter == nil || pLimiter.rps <= 0) && (iLimiter == nil || iLimiter.rps <= 0) {
			next.ServeHTTP(w, r)
			return
		}
		// build keys for three scopes
		globalKey := "global"
		pathKey := "path:" + normalizePath(r.URL.Path)
		ipKey := "ip:" + clientIP(r)

		// deny if any scope exceeds
		if gLimiter != nil && gLimiter.rps > 0 {
			if ok, _ := gLimiter.allow(globalKey); !ok {
				_, wait := gLimiter.allow(globalKey)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", wait))
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("rate limit exceeded"))
				return
			}
		}
		if pLimiter != nil && pLimiter.rps > 0 {
			if ok, _ := pLimiter.allow(pathKey); !ok {
				_, wait := pLimiter.allow(pathKey)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", wait))
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("rate limit exceeded"))
				return
			}
		}
		if iLimiter != nil && iLimiter.rps > 0 {
			if ok, _ := iLimiter.allow(ipKey); !ok {
				_, wait := iLimiter.allow(ipKey)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", wait))
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("rate limit exceeded"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func parseFloatEnv(key string) float64 {
	v := os.Getenv(key)
	if v == "" {
		return -1
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
		return f
	}
	return -1
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// request-id propagation: accept client-provided or generate
		reqID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		lg := mylog.New()
		lg.Info("http.req",
			"req_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"userAgent", r.UserAgent(),
			"referer", r.Referer(),
			"remoteIP", clientIP(r),
			"status", rec.status,
			"duration_ms", int(dur/time.Millisecond),
			"bytes", rec.nbytes,
		)
		// metrics: requests and durations (with label normalization + sampling)
		if shouldSample() {
			path := normalizePath(r.URL.Path)
			mkey := r.Method + "|" + path + "|" + fmt.Sprintf("%d", rec.status)
			dkey := r.Method + "|" + path
			metrics.mu.Lock()
			metrics.reqTotal[mkey]++
			metrics.durSum[dkey] += dur.Seconds()
			metrics.durCount[dkey]++
			metrics.mu.Unlock()
		}
	})
}

// Handlers
func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := a.store.ListProjects()
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		if isReadOnly() {
			writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
			return
		}
		var req struct {
			Name     string   `json:"name"`
			RootPath string   `json:"rootPath"`
			Ignore   []string `json:"ignore"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
			return
		}
		if req.Name == "" || req.RootPath == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "name and rootPath required")
			return
		}
		p := a.store.CreateProject(req.Name, req.RootPath, req.Ignore)
		writeJSON(w, http.StatusOK, map[string]string{"projectID": p.ID})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
	}
}

func (a *API) handleIndexRun(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID string           `json:"projectID"`
		Mode      models.IndexMode `json:"mode"`
		MaxFiles  int              `json:"maxFiles"`
		MaxBytes  int64            `json:"maxBytes"`
		Include   []string         `json:"include"`
		Exclude   []string         `json:"exclude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	if req.Mode == "" {
		req.Mode = models.IndexFull
	}
	job, err := a.store.CreateIndexJob(req.ProjectID, req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// 비동기 처리(즉시 완료 스텁 구현)
	go func(id string) {
		_, _ = a.store.SetJobStatus(id, models.JobRunning, nil)
		// fetch project root
		if p, ok := a.store.GetProject(req.ProjectID); ok {
			opt := indexer.Options{MaxFiles: 500, MaxFileSize: 256 * 1024}
			if req.MaxFiles > 0 {
				opt.MaxFiles = req.MaxFiles
			}
			if req.MaxBytes > 0 {
				opt.MaxFileSize = req.MaxBytes
			}
			if len(req.Include) > 0 {
				opt.Include = req.Include
			}
			if len(req.Exclude) > 0 {
				opt.Exclude = req.Exclude
			}
			docs, _ := indexer.Index(p.RootPath, opt)
			// incremental if supported
			var pipe *embedpipe.Pipeline
			if a.emb != nil && a.vs != nil {
				pipe = embedpipe.New(a.emb, a.vs)
			}
			if inc, ok := a.store.(IncrementalStore); ok {
				present := make([]string, 0, len(docs))
				for _, d := range docs {
					doc := inc.UpsertDocument(p.ID, d.Path, d.Content, d.SHA, d.Lang, d.MTime)
					if pipe != nil {
						pipe.Add(p.ID, doc.ID, d.Path, d.SHA, d.Content)
					}
					present = append(present, d.Path)
				}
				_ = inc.PruneDocuments(p.ID, present)
				if pipe != nil {
					_ = pipe.Flush(context.Background())
				}
			} else {
				for _, d := range docs {
					a.store.AddDocument(p.ID, d.Path, d.Content)
					if pipe != nil {
						pipe.Add(p.ID, "", d.Path, d.SHA, d.Content)
						_ = pipe.Flush(context.Background())
					}
				}
			}
			stats := map[string]int{"documents": len(docs)}
			_, _ = a.store.SetJobStatus(id, models.JobCompleted, stats)
			return
		}
		_, _ = a.store.SetJobStatus(id, models.JobFailed, map[string]int{"documents": 0})
	}(job.ID)

	writeJSON(w, http.StatusOK, map[string]string{"jobID": job.ID})
}

// SSE streaming version: emits job, progress, completed events while indexing
func (a *API) handleIndexRunStream(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID string           `json:"projectID"`
		Mode      models.IndexMode `json:"mode"`
		MaxFiles  int              `json:"maxFiles"`
		MaxBytes  int64            `json:"maxBytes"`
		Include   []string         `json:"include"`
		Exclude   []string         `json:"exclude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	if req.Mode == "" {
		req.Mode = models.IndexFull
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	job, err := a.store.CreateIndexJob(req.ProjectID, req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	_, _ = a.store.SetJobStatus(job.ID, models.JobRunning, nil)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, _ := w.(http.Flusher)
	send := func(event, data string) {
		fmt.Fprintf(w, "event: %s\n", event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if fl != nil {
			fl.Flush()
		}
	}
	send("job", job.ID)

	// perform indexing (collection phase)
	opt := indexer.Options{MaxFiles: 500, MaxFileSize: 256 * 1024}
	if req.MaxFiles > 0 {
		opt.MaxFiles = req.MaxFiles
	}
	if req.MaxBytes > 0 {
		opt.MaxFileSize = req.MaxBytes
	}
	if len(req.Include) > 0 {
		opt.Include = req.Include
	}
	if len(req.Exclude) > 0 {
		opt.Exclude = req.Exclude
	}
	docs, err := indexer.Index(p.RootPath, opt)
	if err != nil {
		send("error", jsonEscape(err.Error()))
		return
	}
	total := len(docs)
	if total == 0 {
		_, _ = a.store.SetJobStatus(job.ID, models.JobCompleted, map[string]int{"documents": 0})
		send("completed", `{"documents":0}`)
		return
	}
	// ingestion phase with progress, respect client cancel
	reqCtx := r.Context()
	ingested := 0
	var pipe *embedpipe.Pipeline
	if a.emb != nil && a.vs != nil {
		pipe = embedpipe.New(a.emb, a.vs)
	}
	if inc, ok := a.store.(IncrementalStore); ok {
		present := make([]string, 0, total)
		for _, d := range docs {
			if reqCtx.Err() != nil {
				return
			}
			doc := inc.UpsertDocument(p.ID, d.Path, d.Content, d.SHA, d.Lang, d.MTime)
			if pipe != nil {
				pipe.Add(p.ID, doc.ID, d.Path, d.SHA, d.Content)
			}
			present = append(present, d.Path)
			ingested++
			if ingested%10 == 0 || ingested == total {
				send("progress", fmt.Sprintf(`{"indexed":%d,"total":%d}`, ingested, total))
			}
		}
		_ = inc.PruneDocuments(p.ID, present)
		if pipe != nil {
			_ = pipe.Flush(reqCtx)
		}
	} else {
		for _, d := range docs {
			if reqCtx.Err() != nil {
				return
			}
			a.store.AddDocument(p.ID, d.Path, d.Content)
			// best-effort embeddings on full-doc content if possible
			if pipe != nil {
				pipe.Add(p.ID, "", d.Path, d.SHA, d.Content)
				_ = pipe.Flush(reqCtx)
			}
			ingested++
			if ingested%10 == 0 || ingested == total {
				send("progress", fmt.Sprintf(`{"indexed":%d,"total":%d}`, ingested, total))
			}
		}
	}
	stats := map[string]int{"documents": total}
	_, _ = a.store.SetJobStatus(job.ID, models.JobCompleted, stats)
	// completed
	send("completed", fmt.Sprintf(`{"documents":%d}`, total))
}

func (a *API) handleIndexJob(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	// path: /index/jobs/:id
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/index/jobs/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "job id required")
		return
	}
	id := parts[0]
	if job, ok := a.store.GetJob(id); ok {
		writeJSON(w, http.StatusOK, job)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "job not found")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func writeError(w http.ResponseWriter, status int, errStr, message string) {
	writeJSON(w, status, apiError{Error: errStr, Message: message, Code: status})
}

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "q required")
		return
	}
	k := 10
	pid := r.URL.Query().Get("projectID")
	results := a.store.Search(pid, q, k)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// Web enrichment (optional)
type webResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

func (a *API) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		Query string
		Limit int
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	var results []webResult
	// mock provider controlled by env (no external network in tests)
	if os.Getenv("MYCODER_WEB_SEARCH_MOCK") == "1" {
		for i := 1; i <= req.Limit; i++ {
			results = append(results, webResult{
				Title:   fmt.Sprintf("Result %d for %s", i, req.Query),
				URL:     fmt.Sprintf("https://example.com/%s/%d", strings.ReplaceAll(req.Query, " ", "-"), i),
				Snippet: "This is a mock snippet.",
				Score:   1.0 - float64(i-1)*0.1,
			})
		}
	} else {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "web search not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (a *API) handleWebIngest(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID  string      `json:"projectID"`
		Results    []webResult `json:"results"`
		MinScore   float64     `json:"minScore"`
		Dedupe     bool        `json:"dedupe"`
		TTLDays    int         `json:"ttlDays"`
		PinOnAdd   bool        `json:"pinOnAdd"`
		Summarize  bool        `json:"summarize"`
		SummaryPin bool        `json:"summaryPin"`
		Query      string      `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || len(req.Results) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and results required")
		return
	}
	if req.MinScore == 0 {
		req.MinScore = 0.0
	}
	seen := map[string]bool{}
	added := 0
	for _, r0 := range req.Results {
		if r0.URL == "" || r0.Score < req.MinScore {
			continue
		}
		key := strings.ToLower(r0.URL)
		if req.Dedupe {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		// add to knowledge with sourceType=web; trustScore from score
		title := r0.Title
		text := r0.Snippet
		if title == "" {
			title = r0.URL
		}
		if text == "" {
			text = r0.URL
		}
		tr := r0.Score
		if tr < 0 {
			tr = 0
		}
		if tr > 1 {
			tr = 1
		}
		k, err := a.store.AddKnowledge(req.ProjectID, "web", r0.URL, title, text, tr, req.PinOnAdd)
		if err == nil {
			if ss, ok := a.store.(*store.SQLiteStore); ok {
				tags := map[string]string{}
				if d := domainFromURL(r0.URL); d != "" {
					tags["domain"] = d
				}
				if req.TTLDays > 0 {
					tags["ttlUntil"] = time.Now().Add(time.Duration(req.TTLDays) * 24 * time.Hour).Format(time.RFC3339)
				}
				if len(tags) > 0 {
					tb, _ := json.Marshal(tags)
					_, _ = ss.DB().Exec(`UPDATE knowledge SET tags=? WHERE id=?`, string(tb), k.ID)
				}
			}
			added++
		}
	}
	if req.Summarize {
		sumText := ""
		if a.llm != nil {
			var b strings.Builder
			for i, r0 := range req.Results {
				if i >= 8 {
					break
				}
				fmt.Fprintf(&b, "- %s (%s): %s\n", strings.TrimSpace(r0.Title), r0.URL, strings.TrimSpace(r0.Snippet))
			}
			sys := llm.Message{Role: llm.RoleSystem, Content: "Summarize these web search results into a concise brief (bullet points)."}
			usr := llm.Message{Role: llm.RoleUser, Content: b.String()}
			st, err := a.llm.Chat(r.Context(), os.Getenv("MYCODER_CHAT_MODEL"), []llm.Message{sys, usr}, false, 0)
			if err == nil {
				defer st.Close()
				var buf strings.Builder
				for {
					d, done, e := st.Recv()
					if e != nil {
						break
					}
					buf.WriteString(d)
					if done {
						break
					}
				}
				sumText = buf.String()
			}
		}
		if sumText == "" {
			sumText = "Summary of web search results."
		}
		title := "Web Summary"
		if strings.TrimSpace(req.Query) != "" {
			title = "Web Summary: " + req.Query
		}
		if k, err := a.store.AddKnowledge(req.ProjectID, "web", "", title, sumText, 0.6, req.SummaryPin); err == nil {
			added++
			if ss, ok := a.store.(*store.SQLiteStore); ok {
				tags := map[string]string{"kind": "summary"}
				if strings.TrimSpace(req.Query) != "" {
					tags["query"] = req.Query
				}
				tb, _ := json.Marshal(tags)
				_, _ = ss.DB().Exec(`UPDATE knowledge SET tags=? WHERE id=?`, string(tb), k.ID)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

func domainFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := strings.TrimSpace(u.Host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return h
}

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Content negotiation: default to Prometheus text exposition.
	// Use JSON when explicitly requested via query or Accept header.
	format := strings.ToLower(r.URL.Query().Get("format"))
	accept := r.Header.Get("Accept")
	if format == "json" || strings.Contains(accept, "application/json") {
		writeJSON(w, http.StatusOK, a.store.Stats())
		return
	}

	st := a.store.Stats()
	val := func(key string) int {
		if v, ok := st[key]; ok {
			return v
		}
		return 0
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	// minimal exposition format
	// project/doc/job/knowledge gauges
	io.WriteString(w, "# HELP mycoder_projects Number of projects.\n")
	io.WriteString(w, "# TYPE mycoder_projects gauge\n")
	io.WriteString(w, fmt.Sprintf("mycoder_projects %d\n", val("projects")))

	io.WriteString(w, "# HELP mycoder_documents Number of indexed documents.\n")
	io.WriteString(w, "# TYPE mycoder_documents gauge\n")
	io.WriteString(w, fmt.Sprintf("mycoder_documents %d\n", val("documents")))

	io.WriteString(w, "# HELP mycoder_jobs Number of index jobs.\n")
	io.WriteString(w, "# TYPE mycoder_jobs gauge\n")
	io.WriteString(w, fmt.Sprintf("mycoder_jobs %d\n", val("jobs")))

	io.WriteString(w, "# HELP mycoder_knowledge Number of knowledge items.\n")
	io.WriteString(w, "# TYPE mycoder_knowledge gauge\n")
	io.WriteString(w, fmt.Sprintf("mycoder_knowledge %d\n", val("knowledge")))

	// http request metrics (counters and duration sum/count)
	metrics.mu.Lock()
	// requests total
	for key, v := range metrics.reqTotal {
		parts := strings.Split(key, "|")
		if len(parts) == 3 {
			method, path, status := parts[0], parts[1], parts[2]
			io.WriteString(w, "# TYPE mycoder_http_requests_total counter\n")
			io.WriteString(w, fmt.Sprintf("mycoder_http_requests_total{method=\"%s\",path=\"%s\",status=\"%s\"} %d\n", method, path, status, v))
		}
	}
	// durations
	for key, sum := range metrics.durSum {
		cnt := metrics.durCount[key]
		parts := strings.Split(key, "|")
		if len(parts) == 2 {
			method, path := parts[0], parts[1]
			io.WriteString(w, "# TYPE mycoder_http_request_duration_seconds summary\n")
			io.WriteString(w, fmt.Sprintf("mycoder_http_request_duration_seconds_sum{method=\"%s\",path=\"%s\"} %f\n", method, path, sum))
			io.WriteString(w, fmt.Sprintf("mycoder_http_request_duration_seconds_count{method=\"%s\",path=\"%s\"} %d\n", method, path, cnt))
		}
	}
	// chat metrics stubs
	io.WriteString(w, "# TYPE mycoder_chat_requests_total counter\n")
	io.WriteString(w, fmt.Sprintf("mycoder_chat_requests_total %d\n", metrics.chatRequests))
	io.WriteString(w, "# TYPE mycoder_chat_stream_tokens_total counter\n")
	io.WriteString(w, fmt.Sprintf("mycoder_chat_stream_tokens_total %d\n", metrics.chatTokens))
	io.WriteString(w, "# HELP mycoder_embed_cache_hits_total Embedding cache hits.\n")
	io.WriteString(w, "# TYPE mycoder_embed_cache_hits_total counter\n")
	io.WriteString(w, fmt.Sprintf("mycoder_embed_cache_hits_total %d\n", metrics.embedCacheHits))
	io.WriteString(w, "# HELP mycoder_embed_cache_misses_total Embedding cache misses.\n")
	io.WriteString(w, "# TYPE mycoder_embed_cache_misses_total counter\n")
	io.WriteString(w, fmt.Sprintf("mycoder_embed_cache_misses_total %d\n", metrics.embedCacheMisses))
	io.WriteString(w, "# HELP mycoder_embed_cache_evictions_total Embedding cache evictions (TTL).\n")
	io.WriteString(w, "# TYPE mycoder_embed_cache_evictions_total counter\n")
	io.WriteString(w, fmt.Sprintf("mycoder_embed_cache_evictions_total %d\n", metrics.embedCacheEvict))
	metrics.mu.Unlock()

	// build info
	io.WriteString(w, "# HELP mycoder_build_info Build information.\n")
	io.WriteString(w, "# TYPE mycoder_build_info gauge\n")
	io.WriteString(w, fmt.Sprintf("mycoder_build_info{version=\"%s\",commit=\"%s\"} 1\n", version.Version, version.Commit))
}

// Knowledge handlers
func (a *API) handleKnowledge(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		if isReadOnly() {
			writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
			return
		}
		var req struct {
			ProjectID, SourceType, PathOrURL, Title, Text string
			TrustScore                                    float64
			Pinned                                        bool
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
			return
		}
		if req.ProjectID == "" || req.SourceType == "" || req.Text == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "projectID, sourceType, text required")
			return
		}
		k, err := a.store.AddKnowledge(req.ProjectID, req.SourceType, req.PathOrURL, req.Title, req.Text, req.TrustScore, req.Pinned)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, k)
	case http.MethodGet:
		pid := r.URL.Query().Get("projectID")
		if pid == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
			return
		}
		min := 0.0
		list, err := a.store.ListKnowledge(pid, min)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"knowledge": list})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
	}
}

func (a *API) handleKnowledgeVet(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct{ ProjectID string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	n, err := a.store.VetKnowledge(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"vettedCount": n})
}

func (a *API) handleKnowledgePromote(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID, Title, Text, PathOrURL, CommitSHA, Files, Symbols string
		Pin                                                          bool
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Text == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and text required")
		return
	}
	k, err := a.store.PromoteKnowledge(req.ProjectID, req.Title, req.Text, req.PathOrURL, req.CommitSHA, req.Files, req.Symbols, req.Pin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (a *API) handleKnowledgeReverify(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct{ ProjectID string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	n, err := a.store.ReverifyKnowledge(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

func (a *API) handleKnowledgeGC(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID string  `json:"projectID"`
		Min       float64 `json:"minScore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	n, err := a.store.GCKnowledge(req.ProjectID, req.Min)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

// Auto-promote: summarize given files with LLM (if configured) and create Knowledge.
func (a *API) handleKnowledgePromoteAuto(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID string
		Files     []string
		Title     string
		Pin       bool
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	if len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "files required")
		return
	}
	// read snippets from files (cap budget)
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	var b strings.Builder
	budget := 4000
	for _, rel := range req.Files {
		_, full, ok := a.resolveProjectPath(req.ProjectID, rel)
		if !ok {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		header := fmt.Sprintf("\n=== %s ===\n", rel)
		if budget-len(header) <= 0 {
			break
		}
		b.WriteString(header)
		budget -= len(header)
		// include up to 800 chars per file
		max := 800
		if max > len(data) {
			max = len(data)
		}
		chunk := string(data[:max])
		if budget-len(chunk) <= 0 {
			break
		}
		b.WriteString(chunk)
		budget -= len(chunk)
	}
	content := b.String()
	summary := ""
	// use LLM if available
	if a.llm != nil && content != "" {
		sys := llm.Message{Role: llm.RoleSystem, Content: "You are a senior engineer. Summarize the following code changes into a concise 'CodeCard' (purpose, approach, key decisions, trade-offs). Keep it under 800 chars."}
		usr := llm.Message{Role: llm.RoleUser, Content: content}
		st, err := a.llm.Chat(r.Context(), os.Getenv("MYCODER_CHAT_MODEL"), []llm.Message{sys, usr}, false, 0)
		if err == nil {
			defer st.Close()
			var buf strings.Builder
			for {
				d, done, e := st.Recv()
				if e != nil {
					break
				}
				buf.WriteString(d)
				if done {
					break
				}
			}
			summary = buf.String()
		}
	}
	if summary == "" { // fallback naive summary
		summary = "CodeCard: Summary of files\n" + content
	}
	title := req.Title
	if title == "" {
		title = fmt.Sprintf("Promoted knowledge: %d files", len(req.Files))
	}
	filesCSV := strings.Join(req.Files, ",")
	k, err := a.store.PromoteKnowledge(req.ProjectID, title, summary, p.RootPath, "", filesCSV, "", req.Pin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, k)
}

// POST /tools/hooks: run project hooks (fmt-check, test, lint) in project root.
func (a *API) handleToolsHooks(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct {
		ProjectID  string            `json:"projectID"`
		Targets    []string          `json:"targets"`
		TimeoutSec int               `json:"timeoutSec"`
		Env        map[string]string `json:"env"`
		Artifact   string            `json:"artifactPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		http.Error(w, "project not found", http.StatusBadRequest)
		return
	}
	targets := req.Targets
	if len(targets) == 0 {
		targets = []string{"fmt-check", "test", "lint"}
	}
	out := map[string]HooksResult{}
	timeout := 60 * time.Second
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	for _, t := range targets {
		// use system make; run each target separately
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", "make "+shellQuote(t))
		cmd.Dir = p.RootPath
		// apply env whitelist
		allowed := map[string]bool{"GOFLAGS": true}
		env := os.Environ()
		for k, v := range req.Env {
			if allowed[k] {
				env = append(env, fmt.Sprintf("%s=%s", k, v))
			}
		}
		cmd.Env = env
		start := time.Now()
		b, err := cmd.CombinedOutput()
		dur := time.Since(start)
		// capture context error before cancel
		ctxErr := ctx.Err()
		cancel()
		ok := err == nil
		rstr := string(b)
		sug := hintFromOutput(t, rstr)
		reason := detectHookReason(t, rstr, ok)
		if !ok {
			// augment with timeout/killed detection
			if ctxErr == context.DeadlineExceeded || (err != nil && strings.Contains(strings.ToLower(err.Error()), "killed")) {
				if sug == "" {
					sug = "타임아웃이 발생했습니다. --timeout 값을 늘려보세요."
				}
				if reason == "" {
					reason = "timeout"
				}
			}
		}
		out[t] = HooksResult{
			Ok:         ok,
			Output:     rstr,
			Suggestion: sug,
			Reason:     reason,
			DurationMs: int(dur.Milliseconds()),
			Lines:      countLines(rstr),
			Bytes:      len(b),
		}
		if !ok {
			// stop on first failure to follow gate behavior
			break
		}
	}
	// add synthetic summary across executed targets
	{
		totalMs, totalLines, totalBytes := 0, 0, 0
		nOk, nFail := 0, 0
		firstFail := ""
		firstFailSug := ""
		for _, t := range targets {
			if r, exists := out[t]; exists {
				totalMs += r.DurationMs
				totalLines += r.Lines
				totalBytes += r.Bytes
				if r.Ok {
					nOk++
				} else {
					nFail++
					if firstFail == "" {
						firstFail = t
						firstFailSug = r.Suggestion
					}
				}
			}
		}
		overallOk := nFail == 0 && len(out) > 0
		msg := fmt.Sprintf("targets=%d ok=%d fail=%d", len(out), nOk, nFail)
		if firstFail != "" {
			msg += ", firstFail=" + firstFail
		}
		out["_summary"] = HooksResult{
			Ok:         overallOk,
			Output:     msg,
			Suggestion: firstFailSug,
			Reason:     "summary",
			DurationMs: totalMs,
			Lines:      totalLines,
			Bytes:      totalBytes,
		}
	}
	// optionally save artifact JSON to project-relative path
	if strings.TrimSpace(req.Artifact) != "" {
		saveHooksArtifact(p.RootPath, req.ProjectID, req.Targets, out, req.Artifact)
	}
	writeJSON(w, http.StatusOK, out)
}

// Minimal MCP-like tools registry (safe, demo-level)
type mcpParam struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string|number|boolean
	Required bool   `json:"required"`
}

type mcpTool struct {
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Params       []string   `json:"params"`
	ParamsSchema []mcpParam `json:"paramsSchema"`
}

func allowedToolsFromEnv() map[string]bool {
	v := strings.TrimSpace(os.Getenv("MYCODER_MCP_ALLOWED_TOOLS"))
	if v == "" {
		return nil
	}
	out := map[string]bool{}
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

// mcpAuthorized enforces tool-level policy:
//   - Allowlist via env MYCODER_MCP_ALLOWED_TOOLS (csv). If set, only listed tools are allowed.
//   - Optional scope via env MYCODER_MCP_REQUIRED_SCOPE (e.g., "mcp:call").
//     Requires header X-MYCODER-Scope to equal REQUIRED_SCOPE+":"+tool.
func mcpAuthorized(r *http.Request, tool string) (bool, string) {
	allow := allowedToolsFromEnv()
	if allow != nil && !allow[tool] {
		return false, "tool not allowed"
	}
	if base := strings.TrimSpace(os.Getenv("MYCODER_MCP_REQUIRED_SCOPE")); base != "" {
		want := base + ":" + tool
		got := strings.TrimSpace(r.Header.Get("X-MYCODER-Scope"))
		if got != want {
			return false, "missing or invalid scope"
		}
	}
	return true, ""
}

func (a *API) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	full := []mcpTool{
		{Name: "echo", Description: "Echo back the provided text", Params: []string{"text"}, ParamsSchema: []mcpParam{{Name: "text", Type: "string", Required: true}}},
		{Name: "time", Description: "Return server time RFC3339", Params: []string{}, ParamsSchema: []mcpParam{}},
	}
	// filter by allowlist if provided
	allow := allowedToolsFromEnv()
	tools := make([]mcpTool, 0, len(full))
	for _, t := range full {
		if allow == nil || allow[t.Name] {
			tools = append(tools, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (a *API) handleMCPCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body or missing name")
		return
	}
	if ok, reason := mcpAuthorized(r, req.Name); !ok {
		writeError(w, http.StatusForbidden, "forbidden", reason)
		return
	}
	switch req.Name {
	case "echo":
		// validate params
		v, ok := req.Params["text"]
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "missing param: text"})
			return
		}
		s, ok := v.(string)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "param text must be string"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": s})
	case "time":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": time.Now().Format(time.RFC3339)})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unknown tool"})
	}
}

// saveHooksArtifact writes structured hooks results JSON to a project-relative path, ensuring confinement.
func saveHooksArtifact(root, projectID string, targets []string, results map[string]HooksResult, rel string) {
	if root == "" || rel == "" {
		return
	}
	// sanitize and ensure path stays within root
	abs := filepath.Join(root, rel)
	if relp, err := filepath.Rel(root, abs); err != nil || strings.HasPrefix(relp, "..") {
		return
	}
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	// wrap with metadata
	payload := map[string]any{
		"projectID": projectID,
		"targets":   targets,
		"time":      time.Now().Format(time.RFC3339),
		"results":   results,
	}
	f, err := os.Create(abs)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(payload)
}

func hintFromOutput(target, output string) string {
	lo := strings.ToLower(output)
	switch target {
	case "fmt-check":
		if strings.Contains(lo, "files need formatting") || strings.Contains(lo, "gofmt") || strings.Contains(lo, "formatted") {
			return "포맷팅을 적용하세요: make fmt"
		}
	case "test":
		if strings.Contains(lo, "--- fail") || strings.Contains(lo, "fail\t") || strings.Contains(lo, "error") || strings.Contains(lo, "exit status") {
			// common go test issues
			if strings.Contains(lo, "panic:") {
				return "패닉 발생 원인을 확인하세요. 스택트레이스를 따라 수정 후 go test ./... -v"
			}
			if strings.Contains(lo, "data race") {
				return "데이터 레이스가 감지되었습니다: go test -race ./... 로 재현하고 동기화를 수정하세요"
			}
			if strings.Contains(lo, "no go files in") || strings.Contains(lo, "build constraints exclude all go files") {
				return "테스트 대상이 비어있습니다. 패키지 경로나 빌드 태그를 확인하세요."
			}
			if strings.Contains(lo, "no required module provides package") || strings.Contains(lo, "cannot find module providing package") {
				return "모듈 의존성 누락: go mod tidy 또는 go get 으로 의존성을 정리하세요"
			}
			return "실패한 테스트를 확인하세요: go test ./... -v (필요 시 -run 으로 타겟팅)"
		}
	case "lint":
		if strings.Contains(lo, "vet") || strings.Contains(lo, "warning") || strings.Contains(lo, "error") || strings.Contains(lo, "undeclared name") || strings.Contains(lo, "unused ") || strings.Contains(lo, "golangci-lint") {
			if strings.Contains(lo, "undeclared name") || strings.Contains(lo, "cannot find package") {
				return "컴파일 오류(식별자/패키지)를 먼저 해결하세요: go build ./... 후 go vet ./..."
			}
			if strings.Contains(lo, "unused ") {
				return "미사용 코드 정리 필요: 사용하지 않는 변수/임포트를 제거하세요 (go vet ./..., go build ./...)"
			}
			if strings.Contains(lo, "ineffassign") || strings.Contains(lo, "deadcode") {
				return "정적 분석 경고를 수정하세요: 불필요 코드/할당 제거 후 다시 go vet 또는 golangci-lint 실행"
			}
			return "린트/정적 분석 경고를 수정하세요: go vet ./..."
		}
	}
	if strings.Contains(lo, "operation not permitted") {
		return "권한 문제로 실패했습니다. 캐시/권한을 확인하거나 별도 환경에서 실행하세요."
	}
	if strings.Contains(lo, "timeout") || strings.Contains(lo, "signal: killed") {
		return "타임아웃이 발생했습니다. --timeout 값을 늘려보세요."
	}
	if strings.Contains(lo, "fatal error: runtime: out of memory") {
		return "메모리 부족으로 실패했습니다. 테스트 병렬도/데이터 크기를 줄이거나 메모리를 확보하세요."
	}
	return ""
}

// detectHookReason attempts to classify why a hook failed (or warn), to help UIs route users.
func detectHookReason(target, output string, ok bool) string {
	if ok {
		return ""
	}
	lo := strings.ToLower(output)
	switch target {
	case "fmt-check":
		if strings.Contains(lo, "files need formatting") {
			return "fmt-mismatch"
		}
	case "test":
		if strings.Contains(output, "--- FAIL") || strings.Contains(output, "FAIL\t") || strings.Contains(lo, "exit status 1") {
			return "test-fail"
		}
		if strings.Contains(lo, "panic:") {
			return "panic"
		}
		if strings.Contains(lo, "data race") {
			return "race"
		}
		if strings.Contains(lo, "no required module provides package") || strings.Contains(lo, "cannot find module providing package") {
			return "mod-missing"
		}
	case "lint":
		if strings.Contains(lo, "vet:") || strings.Contains(lo, "declared and not used") || strings.Contains(lo, "invalid") {
			return "vet-issue"
		}
		if strings.Contains(lo, "ineffassign") || strings.Contains(lo, "deadcode") || strings.Contains(lo, "golangci-lint") {
			return "lint-issue"
		}
	}
	// generic
	if strings.Contains(lo, "error") || strings.Contains(lo, "fail") {
		return "generic-failure"
	}
	return ""
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	// if last line has no trailing newline, count it as a line
	if s[len(s)-1] != '\n' {
		n++
	}
	return n
}

// FS handlers (project-root confined)
func (a *API) handleFSRead(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct{ ProjectID, Path string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and path required")
		return
	}
	root, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	_ = root
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "path outside project")
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": req.Path, "content": string(b)})
}

func (a *API) handleFSWrite(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct{ ProjectID, Path, Content string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and path required")
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "path outside project")
		return
	}
	if ok, reason := fsAllowed(req.Path); !ok {
		writeError(w, http.StatusForbidden, "forbidden", reason)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct{ ProjectID, Path string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and path required")
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "path outside project")
		return
	}
	if ok, reason := fsAllowed(req.Path); !ok {
		writeError(w, http.StatusForbidden, "forbidden", reason)
		return
	}
	if err := os.Remove(full); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) resolveProjectPath(projectID, rel string) (string, string, bool) {
	p, ok := a.store.GetProject(projectID)
	if !ok {
		return "", "", false
	}
	root := p.RootPath
	full := filepath.Clean(filepath.Join(root, rel))
	// ensure inside root
	pr, err := filepath.Abs(root)
	if err != nil {
		return "", "", false
	}
	pf, err := filepath.Abs(full)
	if err != nil {
		return "", "", false
	}
	if !strings.HasPrefix(pf+string(os.PathSeparator), pr+string(os.PathSeparator)) && pf != pr {
		return "", "", false
	}
	return root, full, true
}

func (a *API) handleFSPatch(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct {
		ProjectID string `json:"projectID"`
		Path      string `json:"path"`
		Hunks     []struct {
			Start   int    `json:"start"`
			Length  int    `json:"length"`
			Replace string `json:"replace"`
		} `json:"hunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		http.Error(w, "path outside project", http.StatusForbidden)
		return
	}
	if ok, reason := fsAllowed(req.Path); !ok {
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// apply hunks in order; assume Start/Length are byte offsets
	buf := b
	offset := 0
	for i, h := range req.Hunks {
		start := h.Start + offset
		end := start + h.Length
		if start < 0 || end < start || end > len(buf) {
			http.Error(w, fmt.Sprintf("invalid hunk %d", i), http.StatusBadRequest)
			return
		}
		// build new buffer
		nb := make([]byte, 0, len(buf)-h.Length+len(h.Replace))
		nb = append(nb, buf[:start]...)
		nb = append(nb, []byte(h.Replace)...)
		nb = append(nb, buf[end:]...)
		// update offset for subsequent hunks
		offset += len(h.Replace) - h.Length
		buf = nb
	}
	if err := os.WriteFile(full, buf, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFSPatchUnified parses a unified diff and returns a dry-run summary.
// For Phase 1, only dryRun is supported (no write side-effects).
func (a *API) handleFSPatchUnified(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID string `json:"projectID"`
		DiffText  string `json:"diffText"`
		DryRun    bool   `json:"dryRun"`
		Yes       bool   `json:"yes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || strings.TrimSpace(req.DiffText) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and diffText required")
		return
	}
	// parse unified diff
	files, err := patch.ParseUnified(req.DiffText)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	type fsum struct {
		Path         string `json:"path"`
		Add          int    `json:"add"`
		Del          int    `json:"del"`
		WrittenBytes int    `json:"writtenBytes"`
		Conflict     string `json:"conflict,omitempty"`
	}
	var list []fsum
	totalAdd, totalDel := 0, 0
	for _, f := range files {
		add, del := 0, 0
		for _, h := range f.Hunks {
			for _, ln := range h.Lines {
				if ln.Kind == patch.Added {
					add++
				}
				if ln.Kind == patch.Deleted {
					del++
				}
			}
		}
		totalAdd += add
		totalDel += del
		p := f.NewPath
		if p == "" {
			p = f.OldPath
		}
		list = append(list, fsum{Path: p, Add: add, Del: del})
	}
	// Dry-run summary or require confirmation
	var reqMap map[string]any
	if err := json.NewDecoder(strings.NewReader("{}")); err == nil {
		_ = reqMap
	}
	// if dry-run or no yes flag -> return summary
	// Note: extend request to accept 'yes' for application
	// (kept optional for backward compatibility)
	// Decode again to capture 'yes' if present
	// We already decoded into req above; treat missing as false.
	if req.DryRun || !req.Yes {
		if !req.DryRun && !req.Yes {
			writeError(w, http.StatusBadRequest, "invalid_request", "confirmation required: set yes=true or use dryRun")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dryRun": true, "files": list, "totalAdd": totalAdd, "totalDel": totalDel})
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	// Apply changes; stop on first conflict
	written := 0
	// determine project root for backups
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	// prepare backup dir
	patchID := fmt.Sprintf("pt-%d-%d", time.Now().UnixNano(), rand.Intn(1000))
	backupDir := filepath.Join(p.RootPath, ".mycoder", "patches", patchID, "files")
	for i := range files {
		f := &files[i]
		// decide operation and target path
		op := "modify"
		rel := f.NewPath
		if f.OldPath == "/dev/null" {
			op = "create"
			rel = f.NewPath
		}
		if f.NewPath == "/dev/null" {
			op = "delete"
			rel = f.OldPath
		}
		if strings.TrimSpace(rel) == "" {
			rel = f.OldPath
		}
		_, full, ok := a.resolveProjectPath(req.ProjectID, rel)
		if !ok {
			list[i].Conflict = "path outside project"
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "files": list, "totalAdd": totalAdd, "totalDel": totalDel})
			return
		}
		b, err := os.ReadFile(full)
		if err != nil {
			if op == "create" {
				b = []byte("")
			} else {
				list[i].Conflict = "file not found"
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "files": list, "totalAdd": totalAdd, "totalDel": totalDel})
				return
			}
		}
		// backup original content
		bkp := filepath.Join(backupDir, rel)
		if err := os.MkdirAll(filepath.Dir(bkp), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := os.WriteFile(bkp, b, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		newContent, addLines, delLines, err := patch.ApplyToContentOpt(string(b), f.Hunks, patch.ApplyOptions{IgnoreWhitespace: strings.Contains(strings.ToLower(r.URL.RawQuery), "ignorews=1")})
		if err != nil {
			list[i].Conflict = err.Error()
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "files": list, "totalAdd": totalAdd, "totalDel": totalDel})
			return
		}
		if addLines != list[i].Add || delLines != list[i].Del {
			list[i].Conflict = "stats mismatch"
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "files": list, "totalAdd": totalAdd, "totalDel": totalDel})
			return
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if op == "delete" {
			if err := os.Remove(full); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			list[i].WrittenBytes = 0
		} else {
			if err := os.WriteFile(full, []byte(newContent), 0o644); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			list[i].WrittenBytes = len(newContent)
			written += len(newContent)
		}
	}
	// record patch if sqlite
	if ss, ok := a.store.(*store.SQLiteStore); ok {
		meta := map[string]any{"type": "unified", "files": list, "diffTextBytes": len(req.DiffText)}
		mb, _ := json.Marshal(meta)
		_, _ = ss.DB().Exec(`INSERT INTO patches(id,project_id,path,hunks,applied,created_at,applied_at) VALUES(?,?,?,?,?,?,?)`,
			patchID, req.ProjectID, "<multi>", string(mb), 1, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "patchID": patchID, "files": list, "totalAdd": totalAdd, "totalDel": totalDel, "writtenBytes": written})
}

// Rollback previously applied unified patch using backups stored under .mycoder/patches/<patchID>/files.
func (a *API) handleFSPatchUnifiedRollback(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct {
		ProjectID, PatchID string
		DryRun             bool `json:"dryRun"`
		Yes                bool `json:"yes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.PatchID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and patchID required")
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	backupRoot := filepath.Join(p.RootPath, ".mycoder", "patches", req.PatchID, "files")
	var files []string
	_ = filepath.Walk(backupRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(backupRoot, path)
		files = append(files, rel)
		return nil
	})
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "backup not found")
		return
	}
	if req.DryRun || !req.Yes {
		if !req.DryRun && !req.Yes {
			writeError(w, http.StatusBadRequest, "invalid_request", "confirmation required: set yes=true or use dryRun")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dryRun": true, "files": files})
		return
	}
	written := 0
	for _, rel := range files {
		src := filepath.Join(backupRoot, rel)
		_, dst, ok := a.resolveProjectPath(req.ProjectID, rel)
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden", "path outside project")
			return
		}
		b, err := os.ReadFile(src)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		written += len(b)
	}
	if ss, ok := a.store.(*store.SQLiteStore); ok {
		_, _ = ss.DB().Exec(`UPDATE patches SET applied=0 WHERE id=?`, req.PatchID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": len(files), "writtenBytes": written})
}

// handleFSDiff returns a unified diff between the current file content and provided newContent.
func (a *API) handleFSDiff(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID, Path, NewContent string
		Context                     int
		IgnoreCRLF                  bool
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and path required")
		return
	}
	_, full, ok := a.resolveProjectPath(req.ProjectID, req.Path)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "path outside project")
		return
	}
	oldB, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	diff := patch.GenerateUnified(string(oldB), req.NewContent, req.Path, req.Context, req.IgnoreCRLF)
	writeJSON(w, http.StatusOK, map[string]any{"diffText": diff})
}

func (a *API) handleShellExec(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct {
		ProjectID, Cmd string
		Args           []string
		TimeoutSec     int
		Cwd            string            `json:"cwd"`
		Env            map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Cmd == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and cmd required")
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	to := time.Duration(30) * time.Second
	if req.TimeoutSec > 0 {
		to = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), to)
	defer cancel()
	// Build a zsh -lc commandline so users can use shell semantics.
	cmdline := buildCmdline(req.Cmd, req.Args)
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", cmdline)
	if ok, reason := shellAllowed(cmdline); !ok {
		writeError(w, http.StatusForbidden, "forbidden", reason)
		return
	}
	// resolve cwd under project root if provided
	workdir := p.RootPath
	if strings.TrimSpace(req.Cwd) != "" {
		_, full, ok := a.resolveProjectPath(p.ID, req.Cwd)
		if ok {
			workdir = full
		}
	}
	cmd.Dir = workdir
	// whitelist env pass-through
	allowed := map[string]bool{"GOFLAGS": true, "GOWORK": true, "CGO_ENABLED": true}
	env := os.Environ()
	for k, v := range req.Env {
		if allowed[k] {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	cmd.Env = env
	cb := newCapBuffer(64 * 1024)
	cmd.Stdout = cb
	cmd.Stderr = cb
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"exitCode": exit, "output": string(cb.b), "truncated": cb.truncated, "outputBytes": cb.n, "outputLines": cb.lines})
}

func (a *API) handleShellExecStream(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	var req struct {
		ProjectID, Cmd string
		Args           []string
		TimeoutSec     int
		Cwd            string            `json:"cwd"`
		Env            map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed request body")
		return
	}
	if req.ProjectID == "" || req.Cmd == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and cmd required")
		return
	}
	p, ok := a.store.GetProject(req.ProjectID)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "project not found")
		return
	}
	to := time.Duration(60) * time.Second
	if req.TimeoutSec > 0 {
		to = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), to)
	defer cancel()
	cmdline := buildCmdline(req.Cmd, req.Args)
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", cmdline)
	if ok, _ := shellAllowed(cmdline); !ok {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, "event: error\n")
		fmt.Fprintf(w, "data: %s\n\n", jsonEscape("command blocked by policy"))
		fmt.Fprintf(w, "event: exit\n")
		fmt.Fprintf(w, "data: 126\n\n")
		if fl != nil {
			fl.Flush()
		}
		return
	}
	workdir := p.RootPath
	if strings.TrimSpace(req.Cwd) != "" {
		_, full, ok := a.resolveProjectPath(p.ID, req.Cwd)
		if ok {
			workdir = full
		}
	}
	cmd.Dir = workdir
	allowed := map[string]bool{"GOFLAGS": true, "GOWORK": true, "CGO_ENABLED": true}
	env := os.Environ()
	for k, v := range req.Env {
		if allowed[k] {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	cmd.Env = env
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, _ := w.(http.Flusher)
	send := func(event, data string) {
		fmt.Fprintf(w, "event: %s\n", event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if fl != nil {
			fl.Flush()
		}
	}
	// streaming output limit (64KiB) across stdout/stderr
	var mu sync.Mutex
	limit := 64 * 1024
	sent := 0
	limited := false
	lines := 0
	sendWithLimit := func(kind, data string) {
		mu.Lock()
		if limited {
			mu.Unlock()
			return
		}
		if kind == "stdout" || kind == "stderr" {
			lines++
			remain := limit - sent
			if remain <= 0 {
				limited = true
				mu.Unlock()
				send("limit", "output truncated")
				cancel()
				return
			}
			if len(data) > remain {
				part := data[:remain]
				sent += len(part)
				mu.Unlock()
				send(kind, part)
				send("limit", "output truncated")
				cancel()
				return
			}
			sent += len(data)
		}
		mu.Unlock()
		send(kind, data)
	}
	go streamReader(stdout, func(line string) { sendWithLimit("stdout", line) })
	go streamReader(stderr, func(line string) { sendWithLimit("stderr", line) })
	err := cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	// summary before exit
	send("summary", fmt.Sprintf(`{"bytes":%d,"lines":%d,"limited":%v}`, sent, lines, limited))
	send("exit", fmt.Sprintf("%d", code))
}

func streamReader(r io.Reader, fn func(string)) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			fn(strings.TrimRight(string(buf[:n]), "\n"))
		}
		if err != nil {
			return
		}
	}
}

// buildCmdline concatenates command and args with basic shell-safe quoting for zsh -lc.
func buildCmdline(cmd string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, shellQuote(cmd))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// simple single-quote escaping: ' -> '\''
	if strings.IndexByte(s, '\'') == -1 && strings.IndexByte(s, ' ') == -1 && strings.IndexAny(s, "\t\n$") == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellQuoteOrEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return shellQuote(s)
}

// POST /chat: {messages:[{role,content}], model?, stream?, temperature?}
func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if a.llm == nil {
		http.Error(w, "llm provider not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Messages    []llm.Message `json:"messages"`
		Model       string        `json:"model"`
		Stream      bool          `json:"stream"`
		Temperature float32       `json:"temperature"`
		ProjectID   string        `json:"projectID"`
		Retrieval   struct {
			K int `json:"k"`
		} `json:"retrieval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	msgs := req.Messages
	if req.ProjectID != "" {
		k := req.Retrieval.K
		if k <= 0 {
			k = 5
		}
		msgs = a.withRAGContext(msgs, req.ProjectID, k)
	}
	// optional: summarize conversation if too long (map-reduce style pre-summary)
	msgs = a.maybeSummarize(msgs, req.ProjectID)
	// debug: log first message role/size if enabled
	if os.Getenv("MYCODER_RAG_DEBUG") == "1" {
		role := "(none)"
		size := 0
		if len(msgs) > 0 {
			role = string(msgs[0].Role)
			size = len(msgs[0].Content)
		}
		fmt.Fprintf(os.Stderr, "[rag-debug] messages=%d first_role=%s first_size=%d\n", len(msgs), role, size)
	}
	// metrics: count chat requests
	metrics.mu.Lock()
	metrics.chatRequests++
	metrics.mu.Unlock()

	// apply sliding window after RAG context; keep system rules first
	msgs = slidingWindow(msgs)
	st, err := a.llm.Chat(r.Context(), req.Model, msgs, req.Stream, req.Temperature)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer st.Close()
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		for {
			delta, done, err := st.Recv()
			if err != nil {
				fmt.Fprintf(w, "event: error\n")
				fmt.Fprintf(w, "data: %s\n\n", jsonEscape(err.Error()))
				if fl != nil {
					fl.Flush()
				}
				return
			}
			if delta != "" {
				fmt.Fprintf(w, "event: token\n")
				fmt.Fprintf(w, "data: %s\n\n", jsonEscape(delta))
				metrics.mu.Lock()
				metrics.chatTokens += len(delta) / 4
				metrics.mu.Unlock()
				if fl != nil {
					fl.Flush()
				}
			}
			if done {
				fmt.Fprintf(w, "event: done\n\n")
				if fl != nil {
					fl.Flush()
				}
				return
			}
		}
	}
	var buf strings.Builder
	for {
		delta, done, err := st.Recv()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		buf.WriteString(delta)
		if done {
			break
		}
	}
	// approximate token count for non-streaming
	metrics.mu.Lock()
	metrics.chatTokens += len(buf.String()) / 4
	metrics.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"content": buf.String()})
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return string(b)
}

// slidingWindow trims conversation messages to fit a simple character budget,
// keeping system messages first and the most recent user/assistant messages.
func slidingWindow(messages []llm.Message) []llm.Message {
	// budget from env (chars), default ~6000 bytes
	max := 6000
	if v := os.Getenv("MYCODER_CHAT_MAX_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}
	if len(messages) == 0 || max <= 0 {
		return messages
	}
	var systems []llm.Message
	var rest []llm.Message
	for _, m := range messages {
		if m.Role == llm.RoleSystem {
			systems = append(systems, m)
		} else {
			rest = append(rest, m)
		}
	}
	// collect from tail of rest until budget allows
	budget := max
	// account system messages first
	for _, m := range systems {
		budget -= len(m.Content)
	}
	if budget <= 0 {
		return systems
	}
	picked := make([]llm.Message, 0, len(rest))
	total := 0
	for i := len(rest) - 1; i >= 0; i-- {
		c := len(rest[i].Content)
		if total+c > budget {
			break
		}
		picked = append(picked, rest[i])
		total += c
	}
	// reverse picked to chronological order
	for i, j := 0, len(picked)-1; i < j; i, j = i+1, j-1 {
		picked[i], picked[j] = picked[j], picked[i]
	}
	out := make([]llm.Message, 0, len(systems)+len(picked))
	out = append(out, systems...)
	out = append(out, picked...)
	return out
}

// withRAGContext builds a simple context message using lexical search results for the latest user query.
func (a *API) withRAGContext(messages []llm.Message, projectID string, k int) []llm.Message {
	var q string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			q = messages[i].Content
			break
		}
	}
	if strings.TrimSpace(q) == "" {
		return messages
	}
	// adjust retrieval K based on intent
	intent := planner.Classify(q)
	k = planner.RetrievalK(intent, k)
	// Use hybrid retrieval (BM25 + KNN) when embeddings available; fallback to lexical only.
	var raw []models.SearchResult
	if a.emb != nil && a.vs != nil {
		// build hybrid
		lex := retriever.NewBM25(a.store)
		knn := retriever.NewKNN(a.vs, a.emb)
		hyb := retriever.NewHybrid(lex, knn)
		// retrieval timeout configurable via env; default 5s
		rt := 5 * time.Second
		if v := os.Getenv("MYCODER_RETRIEVAL_TIMEOUT_MS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				rt = time.Duration(n) * time.Millisecond
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), rt)
		defer cancel()
		if res, err := hyb.Retrieve(ctx, projectID, q, k*2); err == nil {
			raw = res
		}
	}
	if len(raw) == 0 {
		raw = a.store.Search(projectID, q, k*2)
	}
	if len(raw) == 0 {
		// No hits: inject a concise project overview to orient the model
		if ov := a.projectOverview(projectID, 2000); strings.TrimSpace(ov) != "" {
			sys := llm.Message{Role: llm.RoleSystem, Content: ov}
			out := make([]llm.Message, 0, len(messages)+1)
			out = append(out, sys)
			out = append(out, messages...)
			return out
		}
		return messages
	}
	// trustScore-aware rerank: adjust search score with knowledge trust per path
	trust := make(map[string]float64)
	if kn, err := a.store.ListKnowledge(projectID, 0.0); err == nil {
		for _, kv := range kn {
			if kv.PathOrURL != "" && kv.TrustScore > trust[kv.PathOrURL] {
				trust[kv.PathOrURL] = kv.TrustScore
			}
		}
	}
	type scored struct {
		s   models.SearchResult
		adj float64
	}
	cand := make([]scored, 0, len(raw))
	const alpha = 1.0
	for _, h := range raw {
		adj := h.Score + alpha*trust[h.Path]
		cand = append(cand, scored{s: h, adj: adj})
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].adj > cand[j].adj })
	// group top candidates by path and deduplicate overlapping ranges
	type rng struct{ s, e int }
	grouped := make(map[string][]rng)
	addRange := func(path string, s, e int) bool {
		if s <= 0 {
			s = 1
		}
		if e > 0 && e < s {
			e = s
		}
		rr := grouped[path]
		for _, r := range rr {
			// overlap/adjacent: skip to avoid redundancy
			if (e == 0 || r.e == 0) || !(e < r.s || r.e < s) {
				return false
			}
		}
		grouped[path] = append(rr, rng{s: s, e: e})
		return true
	}
	// fill grouped ranges honoring k budget on unique paths first
	for _, c := range cand {
		p := c.s.Path
		if len(grouped) >= k && grouped[p] == nil {
			continue
		}
		_ = addRange(p, c.s.StartLine, c.s.EndLine)
	}
	// flatten to ordered hits with one or two ranges per path
	paths := make([]string, 0, len(grouped))
	for p := range grouped {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	hits := make([]models.SearchResult, 0, k)
	for _, p := range paths {
		rr := grouped[p]
		// cap ranges per path to 2 for diversity
		if len(rr) > 2 {
			rr = rr[:2]
		}
		for _, r := range rr {
			hits = append(hits, models.SearchResult{Path: p, StartLine: r.s, EndLine: r.e})
			if len(hits) >= k {
				break
			}
		}
		if len(hits) >= k {
			break
		}
	}
	if os.Getenv("MYCODER_RAG_DEBUG") == "1" {
		// log selected paths for context
		fmt.Fprintf(os.Stderr, "[rag-debug] hits=%d\n", len(hits))
		max := hits
		if len(max) > 5 {
			max = hits[:5]
		}
		for _, h := range max {
			fmt.Fprintf(os.Stderr, "[rag-debug] hit %s:%d-%d\n", h.Path, h.StartLine, h.EndLine)
		}
	}
	// prepend curated knowledge heads (titles/links) if exists
	if kn, err := a.store.ListKnowledge(projectID, 0.5); err == nil && len(kn) > 0 {
		var kb strings.Builder
		kb.WriteString("Curated Knowledge:\n")
		max := 3
		if len(kn) < max {
			max = len(kn)
		}
		for i := 0; i < max; i++ {
			kb.WriteString("- ")
			if kn[i].Title != "" {
				kb.WriteString(kn[i].Title)
			} else {
				kb.WriteString(kn[i].PathOrURL)
			}
			kb.WriteString("\n")
		}
		sys := llm.Message{Role: llm.RoleSystem, Content: kb.String()}
		messages = append([]llm.Message{sys}, messages...)
	}
	var b strings.Builder
	b.WriteString(ragInstruction(q))
	b.WriteString("Context:\n")
	// approximate token budget in bytes (dynamic line count per snippet)
	budget := 3000
	if v := os.Getenv("MYCODER_RAG_BUDGET_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			budget = n
		}
	}
	avgLineBytes := 80 // heuristic; used to size maxLines per snippet
	var root string
	if p, ok := a.store.GetProject(projectID); ok {
		root = p.RootPath
	}
	for _, h := range hits {
		loc := h.Path
		if h.StartLine > 0 {
			if h.EndLine > 0 && h.EndLine != h.StartLine {
				loc = fmt.Sprintf("%s:%d-%d", h.Path, h.StartLine, h.EndLine)
			} else {
				loc = fmt.Sprintf("%s:%d", h.Path, h.StartLine)
			}
		}
		b.WriteString("- ")
		b.WriteString(loc)
		b.WriteString("\n")
		if root != "" {
			// dynamic maxLines based on remaining budget
			maxLines := 24
			if avgLineBytes > 0 {
				est := budget / avgLineBytes
				if est < maxLines {
					maxLines = est
				}
				if maxLines < 6 {
					maxLines = 6
				}
			}
			code := readSnippet(root, h.Path, h.StartLine, h.EndLine, maxLines)
			if code != "" {
				block := fmt.Sprintf("```%s\n%s\n```\n", fenceLangFor(h.Path), code)
				if len(block) > budget {
					// trim block content to fit remaining budget, keep fences
					// leave ~8 bytes headroom
					lang := fenceLangFor(h.Path)
					head := 4 + len(lang) // ``` + lang + \n
					tail := 4             // ```\n
					keep := budget - head - tail
					if keep > 0 {
						// extract content portion
						content := code
						if len(content) > keep {
							content = content[:keep]
						}
						block = fmt.Sprintf("```%s\n%s\n```\n", lang, content)
					}
				}
				if budget-len(block) > 0 {
					b.WriteString(block)
					budget -= len(block)
				}
			}
		}
		if budget <= 0 {
			break
		}
	}
	ctxText := b.String()
	// Strategy: default inject as system. Optional: prepend to last user when env set.
	if os.Getenv("MYCODER_RAG_INJECT_STRATEGY") == "append_user" {
		out := make([]llm.Message, 0, len(messages))
		out = append(out, messages...)
		// find last user message
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Role == llm.RoleUser {
				out[i].Content = ctxText + "\n\n" + out[i].Content
				if os.Getenv("MYCODER_RAG_DEBUG") == "1" {
					fmt.Fprintf(os.Stderr, "[rag-debug] injected into last user message, added=%d bytes\n", len(ctxText))
				}
				break
			}
		}
		return out
	}
	sys := llm.Message{Role: llm.RoleSystem, Content: ctxText}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, sys)
	out = append(out, messages...)
	return out
}

// readSnippet reads lines [start:end] with margins; clamps to file bounds.
func readSnippet(root, rel string, start, end, maxLines int) string {
	full := filepath.Clean(filepath.Join(root, rel))
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = start
	}
	margin := 2
	s := start - margin
	if s < 1 {
		s = 1
	}
	e := end + margin
	if e > len(lines) {
		e = len(lines)
	}
	if maxLines > 0 && e-s+1 > maxLines {
		e = s + maxLines - 1
	}
	return strings.Join(lines[s-1:e], "\n")
}

func fenceLangFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx":
		return "js"
	case ".py":
		return "py"
	case ".md":
		return "md"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return ""
	}
}

// ragInstruction returns a style-aware instruction for LLM behavior.
// Controlled via env MYCODER_RAG_STYLE: "detailed" (default) or "concise".
func ragInstruction(userQ string) string {
	style := strings.ToLower(strings.TrimSpace(os.Getenv("MYCODER_RAG_STYLE")))
	if style == "concise" {
		return "You are a coding assistant. Use the following repo context and cite files with line ranges. Keep answers focused and accurate.\n\n"
	}
	// Try to respond in Korean when the user query contains Hangul
	langHint := ""
	if containsHangul(userQ) {
		langHint = " 답변은 한국어로 작성하세요."
	}
	return "You are a meticulous coding assistant. Provide a thorough, structured answer using the repo context and cite files with line ranges. Include reasoning, key files, related modules, pitfalls, and concrete next steps." + langHint + "\n\n"
}

func containsHangul(s string) bool {
	for _, r := range s {
		if (r >= 0x1100 && r <= 0x11FF) || (r >= 0x3130 && r <= 0x318F) || (r >= 0xAC00 && r <= 0xD7A3) {
			return true
		}
	}
	return false
}

// projectOverview builds a brief, bounded summary of the project structure and key files.
// maxBytes limits the returned string size to avoid over-injecting context.
func (a *API) projectOverview(projectID string, maxBytes int) string {
	p, ok := a.store.GetProject(projectID)
	if !ok || p.RootPath == "" {
		return ""
	}
	// Index a subset of files to summarize.
	docs, err := indexer.Index(p.RootPath, indexer.Options{MaxFiles: 300, MaxFileSize: 128 * 1024})
	if err != nil || len(docs) == 0 {
		return ""
	}
	// Language counts
	langCount := make(map[string]int)
	// Collect top-level dirs/files and a shallow tree (depth <= 2)
	type node struct {
		name  string
		isDir bool
	}
	top := make(map[string][]node) // dir -> children
	rootChildren := make(map[string]bool)
	keyFiles := map[string]bool{"README.md": false, "go.mod": false, "package.json": false, "pyproject.toml": false}
	for _, d := range docs {
		if d.Lang != "" {
			langCount[d.Lang]++
		}
		parts := strings.Split(d.Path, "/")
		if len(parts) == 1 {
			rootChildren[parts[0]] = true
			if _, ok := keyFiles[parts[0]]; ok {
				keyFiles[parts[0]] = true
			}
			continue
		}
		// depth 2 tree
		parent := parts[0]
		child := parts[1]
		lst := top[parent]
		// avoid duplicates, only record first-level children
		seen := false
		for _, n := range lst {
			if n.name == child {
				seen = true
				break
			}
		}
		if !seen {
			// best effort dir/file guess based on extension
			isDir := len(parts) > 2 || filepath.Ext(child) == ""
			top[parent] = append(lst, node{name: child, isDir: isDir})
		}
	}
	// Build overview text
	var b strings.Builder
	b.WriteString("Project Overview (auto):\n")
	b.WriteString("- Root: ")
	b.WriteString(filepath.Base(p.RootPath))
	b.WriteString("\n")
	// Languages summary
	if len(langCount) > 0 {
		b.WriteString("- Languages: ")
		// order by count desc
		type kv struct {
			k string
			v int
		}
		arr := make([]kv, 0, len(langCount))
		for k, v := range langCount {
			arr = append(arr, kv{k, v})
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].v > arr[j].v })
		max := len(arr)
		if max > 6 {
			max = 6
		}
		for i := 0; i < max; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(arr[i].k)
			b.WriteString(": ")
			b.WriteString(strconv.Itoa(arr[i].v))
		}
		b.WriteString("\n")
	}
	// Key files presence
	var keys []string
	for k, ok := range keyFiles {
		if ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		b.WriteString("- Key files: ")
		b.WriteString(strings.Join(keys, ", "))
		b.WriteString("\n")
	}
	// Shallow tree
	b.WriteString("- Structure (depth 2):\n")
	// root children list
	rnames := make([]string, 0, len(rootChildren)+len(top))
	for k := range rootChildren {
		rnames = append(rnames, k)
	}
	for k := range top {
		rnames = append(rnames, k)
	}
	// dedupe
	uniq := make(map[string]bool)
	out := make([]string, 0, len(rnames))
	for _, n := range rnames {
		if !uniq[n] {
			uniq[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	capRoot := 10
	if len(out) < capRoot {
		capRoot = len(out)
	}
	for i := 0; i < capRoot; i++ {
		name := out[i]
		b.WriteString("  • ")
		b.WriteString(name)
		b.WriteString("\n")
		children := top[name]
		if len(children) == 0 {
			continue
		}
		sort.Slice(children, func(i, j int) bool { return children[i].name < children[j].name })
		max := len(children)
		if max > 6 {
			max = 6
		}
		for j := 0; j < max; j++ {
			b.WriteString("    - ")
			b.WriteString(children[j].name)
			if children[j].isDir {
				b.WriteString("/")
			}
			b.WriteString("\n")
		}
	}
	// Append short extracts from README/package/go.mod if present
	addSnippet := func(rel string, maxLines int) {
		full := filepath.Join(p.RootPath, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			return
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		txt := strings.TrimSpace(strings.Join(lines, "\n"))
		if txt == "" {
			return
		}
		b.WriteString("\n")
		b.WriteString(rel)
		b.WriteString(":\n")
		// fence as markdown; LLMs handle plain too
		b.WriteString("" + txt + "\n")
	}
	for _, rel := range []string{"README.md", "package.json", "go.mod"} {
		addSnippet(rel, 40)
		if b.Len() >= maxBytes {
			break
		}
	}
	outStr := b.String()
	if maxBytes > 0 && len(outStr) > maxBytes {
		return outStr[:maxBytes]
	}
	return outStr
}

type cachingEmbedder struct {
	u      llm.Embedder
	mu     sync.Mutex
	data   map[string][]float32
	times  map[string]time.Time
	ttlSec int
	gen    string // cache generation namespace for invalidation
}

func newCachingEmbedder(u llm.Embedder) llm.Embedder {
	ttl := 3600
	if v := os.Getenv("MYCODER_EMBED_CACHE_TTL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ttl = n
		}
	}
	gen := os.Getenv("MYCODER_EMBED_CACHE_GEN")
	return &cachingEmbedder{u: u, data: make(map[string][]float32), times: make(map[string]time.Time), ttlSec: ttl, gen: gen}
}

func (c *cachingEmbedder) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	// check generation bump (env-driven invalidation)
	if g := os.Getenv("MYCODER_EMBED_CACHE_GEN"); g != c.gen {
		c.mu.Lock()
		if g != c.gen { // re-check after lock
			purged := len(c.data)
			c.data = make(map[string][]float32)
			c.times = make(map[string]time.Time)
			c.gen = g
			if purged > 0 {
				metrics.embedCacheEvict += purged
			}
		}
		c.mu.Unlock()
	}
	out := make([][]float32, len(inputs))
	var missIdx []int
	c.mu.Lock()
	for i, s := range inputs {
		key := cacheKey(model, s, c.gen)
		if v, ok := c.data[key]; ok && len(v) > 0 {
			// TTL check
			if c.ttlSec > 0 {
				if t, ok2 := c.times[key]; ok2 {
					if time.Since(t) > time.Duration(c.ttlSec)*time.Second {
						// expired
						missIdx = append(missIdx, i)
						metrics.embedCacheEvict++
						continue
					}
				}
			}
			out[i] = v
			metrics.embedCacheHits++
		} else {
			missIdx = append(missIdx, i)
		}
	}
	c.mu.Unlock()
	if len(missIdx) > 0 {
		req := make([]string, len(missIdx))
		for j, i := range missIdx {
			req[j] = inputs[i]
		}
		vecs, err := c.u.Embeddings(ctx, model, req)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		for j, i := range missIdx {
			if j < len(vecs) {
				v := vecs[j]
				out[i] = v
				key := cacheKey(model, inputs[i], c.gen)
				c.data[key] = v
				c.times[key] = time.Now()
				metrics.embedCacheMisses++
			}
		}
		// enforce optional max entries
		if max := cacheMaxEntries(); max > 0 && len(c.data) > max {
			c.evictOldest(len(c.data) - max)
		}
		c.mu.Unlock()
	}
	return out, nil
}

func cacheKey(model, input, gen string) string {
	if gen != "" {
		return model + "|" + gen + "|" + input
	}
	return model + "|" + input
}

func cacheMaxEntries() int {
	if v := os.Getenv("MYCODER_EMBED_CACHE_MAX_ENTRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func (c *cachingEmbedder) evictOldest(n int) {
	if n <= 0 {
		return
	}
	// find n oldest keys by timestamp (simple O(m) scan)
	type kv struct {
		k string
		t time.Time
	}
	items := make([]kv, 0, len(c.times))
	for k, t := range c.times {
		items = append(items, kv{k: k, t: t})
	}
	// partial selection sort for n oldest
	for i := 0; i < n && i < len(items); i++ {
		minIdx := i
		for j := i + 1; j < len(items); j++ {
			if items[j].t.Before(items[minIdx].t) {
				minIdx = j
			}
		}
		items[i], items[minIdx] = items[minIdx], items[i]
		delete(c.data, items[i].k)
		delete(c.times, items[i].k)
		metrics.embedCacheEvict++
	}
}

// maybeSummarize generates a brief summary system message if conversation exceeds a size threshold.
// Controlled by env:
//
//	MYCODER_CHAT_SUMMARY_ENABLE=1 to enable (default off)
//	MYCODER_CHAT_SUMMARY_THRESHOLD_CHARS (default 8000)
func (a *API) maybeSummarize(messages []llm.Message, projectID string) []llm.Message {
	if os.Getenv("MYCODER_CHAT_SUMMARY_ENABLE") != "1" || a.llm == nil {
		return messages
	}
	// compute total content size (exclude system)
	sum := 0
	for _, m := range messages {
		if m.Role == llm.RoleSystem {
			continue
		}
		sum += len(m.Content)
	}
	thr := 8000
	if v := os.Getenv("MYCODER_CHAT_SUMMARY_THRESHOLD_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			thr = n
		}
	}
	if sum <= thr {
		return messages
	}
	// build compact summary prompt from tail portion
	// include at most last ~6 messages (user/assistant), ignoring system
	var tail []llm.Message
	for i := len(messages) - 1; i >= 0 && len(tail) < 6; i-- {
		if messages[i].Role == llm.RoleSystem {
			continue
		}
		tail = append(tail, messages[i])
	}
	// reverse to chronological order
	for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 {
		tail[i], tail[j] = tail[j], tail[i]
	}
	var b strings.Builder
	b.WriteString("Summarize the following conversation succinctly.\n")
	b.WriteString("- Preserve key decisions and action items.\n")
	b.WriteString("- Include file citations if mentioned (path:line-range).\n")
	b.WriteString("- Korean output if user language is Korean.\n\n")
	for _, m := range tail {
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	prompt := b.String()
	// call LLM non-streaming with low temperature
	st, err := a.llm.Chat(context.Background(), os.Getenv("MYCODER_CHAT_MODEL"), []llm.Message{{Role: llm.RoleUser, Content: prompt}}, false, 0.1)
	if err != nil {
		return messages
	}
	defer st.Close()
	var sb strings.Builder
	for {
		delta, done, e := st.Recv()
		if e != nil {
			break
		}
		sb.WriteString(delta)
		if done {
			break
		}
	}
	summary := strings.TrimSpace(sb.String())
	if summary == "" {
		return messages
	}
	sys := llm.Message{Role: llm.RoleSystem, Content: "Conversation summary:\n" + summary}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, sys)
	out = append(out, messages...)
	return out
}

// handleKnowledgeApprove manually approves knowledge items by id: pin=true and raise trust to minTrust.
func (a *API) handleKnowledgeApprove(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if isReadOnly() {
		writeError(w, http.StatusForbidden, "forbidden", "read-only mode")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	var req struct {
		ProjectID  string
		IDs        []string
		Pin        bool
		MinTrust   float64
		ApprovedBy string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID and ids required")
		return
	}
	if req.MinTrust == 0 {
		req.MinTrust = 0.8
	}
	n, err := a.store.ApproveKnowledge(req.ProjectID, req.IDs, true, req.MinTrust)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approved": n})
}

// handleKnowledgePending lists unpinned knowledge items (optionally filter by sourceType and minTrust).
func (a *API) handleKnowledgePending(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	pid := r.URL.Query().Get("projectID")
	if pid == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "projectID required")
		return
	}
	src := r.URL.Query().Get("sourceType")
	min := 0.0
	if v := r.URL.Query().Get("minTrust"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			min = f
		}
	}
	list, err := a.store.ListKnowledge(pid, min)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	type item struct {
		ID, SourceType, PathOrURL, Title string
		Trust                            float64
	}
	var out []item
	for _, k := range list {
		if k.Pinned {
			continue
		}
		if src != "" && k.SourceType != src {
			continue
		}
		out = append(out, item{ID: k.ID, SourceType: k.SourceType, PathOrURL: k.PathOrURL, Title: k.Title, Trust: k.TrustScore})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": out})
}
