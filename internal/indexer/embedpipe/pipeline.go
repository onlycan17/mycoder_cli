package embedpipe

import (
	"context"
	"os"
	"time"

	"mycoder/internal/llm"
	"mycoder/internal/vectorstore"
)

type item struct {
	projectID string
	docID     string
	path      string
	text      string
	dim       int
	provider  string
	model     string
}

type Pipeline struct {
	emb   llm.Embedder
	vs    vectorstore.VectorStore
	model string
	prov  string
	batch int
	cache map[string]struct{}
	items []item
	tr    Translator
}

func New(emb llm.Embedder, vs vectorstore.VectorStore) *Pipeline {
	if emb == nil || vs == nil {
		return nil
	}
	m := getDefaultModel()
	p := getDefaultProvider()
	return &Pipeline{emb: emb, vs: vs, model: m, prov: p, batch: 8, cache: make(map[string]struct{})}
}

// WithTranslator sets an optional translator used for language fallback.
func (p *Pipeline) WithTranslator(tr Translator) *Pipeline { p.tr = tr; return p }

// Add schedules a document text for embedding. shaKey is used for simple de-dup.
func (p *Pipeline) Add(projectID, docID, path, sha, text string) {
	if p == nil {
		return
	}
	// sha-based cache: skip if seen
	key := projectID + "|" + path + "|" + sha
	if sha != "" {
		if _, ok := p.cache[key]; ok {
			return
		}
		p.cache[key] = struct{}{}
	}
	// truncate overly long text to a conservative size
	if len(text) > 8000 {
		text = text[:8000]
	}
	// pick model/provider per item (code vs document)
	imodel := pickModelForPath(path, p.model)
	iprov := pickProviderForPath(path, p.prov)
	p.items = append(p.items, item{projectID: projectID, docID: docID, path: path, text: text, model: imodel, provider: iprov})
	if len(p.items) >= p.batch {
		_ = p.Flush(context.Background())
	}
}

// Flush embeds pending items and upserts to the vector store. Retries once on failure.
func (p *Pipeline) Flush(ctx context.Context) error {
	if p == nil || len(p.items) == 0 {
		return nil
	}
	// group items by (model, provider) to batch per model
	groups := make(map[string][]int)
	order := make([]string, 0)
	for i, it := range p.items {
		key := it.model + "|" + it.provider
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}
	for _, key := range order {
		idxs := groups[key]
		if len(idxs) == 0 {
			continue
		}
		model, provider := splitKey(key)
		texts := p.textsForGroup(ctx, idxs)
		vecs, err := p.emb.Embeddings(ctx, model, texts)
		if err != nil || len(vecs) != len(texts) {
			// per-item retry
			for _, i := range idxs {
				it := p.items[i]
				t1 := p.textsForGroup(ctx, []int{i})
				v, e := p.emb.Embeddings(ctx, model, t1)
				if e != nil || len(v) == 0 {
					continue
				}
				_ = p.vs.Upsert(ctx, []vectorstore.UpsertItem{{ProjectID: it.projectID, DocID: it.path, ChunkID: it.docID, Vector: v[0], Dim: len(v[0]), Provider: provider, Model: model}})
			}
			continue
		}
		ups := make([]vectorstore.UpsertItem, 0, len(vecs))
		for j, i := range idxs {
			it := p.items[i]
			ups = append(ups, vectorstore.UpsertItem{ProjectID: it.projectID, DocID: it.path, ChunkID: it.docID, Vector: vecs[j], Dim: len(vecs[j]), Provider: provider, Model: model})
		}
		_ = p.vs.Upsert(ctx, ups)
	}
	p.items = p.items[:0]
	return nil
}

// Translator defines a minimal interface for translating text.
type Translator interface {
	Translate(ctx context.Context, srcLang, tgtLang, text string) (string, error)
}

// textsForGroup returns the processed texts for given item indexes, applying translation fallback when enabled.
func (p *Pipeline) textsForGroup(ctx context.Context, idxs []int) []string {
	out := make([]string, len(idxs))
	useFallback := os.Getenv("MYCODER_EMBED_TRANSLATE_FALLBACK") == "1"
	to := "en"
	// timeout
	tmo := 1200 * time.Millisecond
	if v := os.Getenv("MYCODER_EMBED_TRANSLATE_TIMEOUT_MS"); v != "" {
		if ms, err := atoi(v); err == nil && ms > 0 {
			tmo = time.Duration(ms) * time.Millisecond
		}
	}
	for j, i := range idxs {
		txt := p.items[i].text
		if useFallback && p.tr != nil && seemsKorean(txt) {
			c2, cancel := context.WithTimeout(ctx, tmo)
			tr, err := p.tr.Translate(c2, "ko", to, txt)
			cancel()
			if err == nil && tr != "" {
				txt = tr
			}
		}
		out[j] = txt
	}
	return out
}

// seemsKorean returns true if the string contains Hangul codepoints.
func seemsKorean(s string) bool {
	for _, r := range s {
		if (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0x1100 && r <= 0x11FF) || (r >= 0x3130 && r <= 0x318F) {
			return true
		}
	}
	return false
}

func atoi(s string) (int, error) {
	n := 0
	sign := 1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 && (c == '-' || c == '+') {
			if c == '-' {
				sign = -1
			}
			continue
		}
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return sign * n, nil
}

// --- helpers for model/provider selection ---
func getDefaultModel() string {
	if m := os.Getenv("MYCODER_EMBEDDING_MODEL"); m != "" {
		return m
	}
	return "text-embedding-3-small"
}

func getDefaultProvider() string {
	if p := os.Getenv("MYCODER_EMBEDDING_PROVIDER"); p != "" {
		return p
	}
	return "openai"
}

func pickModelForPath(path, def string) string {
	if isCodePath(path) {
		if m := os.Getenv("MYCODER_EMBEDDING_MODEL_CODE"); m != "" {
			return m
		}
	}
	if def != "" {
		return def
	}
	return getDefaultModel()
}

func pickProviderForPath(path, def string) string {
	if isCodePath(path) {
		if p := os.Getenv("MYCODER_EMBEDDING_PROVIDER_CODE"); p != "" {
			return p
		}
	}
	if def != "" {
		return def
	}
	return getDefaultProvider()
}

func isCodePath(path string) bool {
	// allow custom list: comma-separated extensions without dot, e.g. "go,ts,js,py"
	if ex := os.Getenv("MYCODER_EMBEDDING_CODE_EXTS"); ex != "" {
		ext := extOf(path)
		for _, e := range splitComma(ex) {
			if "."+e == ext {
				return true
			}
		}
	}
	switch extOf(path) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rb", ".rs", ".c", ".h", ".cpp", ".cc", ".cs", ".php", ".kt", ".swift", ".m", ".mm", ".scala", ".tscn", ".gd":
		return true
	default:
		return false
	}
}

func extOf(path string) string {
	// cheap ext lookup without importing filepath for tiny dependency surface
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}

func splitComma(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if start < i {
				out = append(out, trimSpace(s[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func splitKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
