package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// KnownKeys defines environment variable keys that mycoder recognizes.
var KnownKeys = []string{
	"MYCODER_SERVER_URL",
	"MYCODER_SQLITE_PATH",
	"MYCODER_LLM_PROVIDER",
	"MYCODER_OPENAI_BASE_URL",
	"MYCODER_OPENAI_API_KEY",
	"MYCODER_CHAT_MODEL",
	"MYCODER_EMBEDDING_MODEL",
	"MYCODER_LLM_MIN_INTERVAL_MS",
	"MYCODER_SHELL_ALLOW_REGEX",
	"MYCODER_SHELL_DENY_REGEX",
	"MYCODER_FS_ALLOW_REGEX",
	"MYCODER_FS_DENY_REGEX",
	"MYCODER_CURATOR_DISABLE",
	"MYCODER_CURATOR_INTERVAL",
	"MYCODER_KNOWLEDGE_MIN_TRUST",
	"MYCODER_METRICS_SAMPLE_RATE",
}

// LoadAndApply loads configuration from ~/.mycoder/config.yaml (or .yml/.json)
// and applies values into the process environment for known keys if they are
// not already set. Environment variables take precedence over file values.
func LoadAndApply() error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil // non-fatal
	}
	base := filepath.Join(home, ".mycoder")
	paths := []string{
		filepath.Join(base, "config.yaml"),
		filepath.Join(base, "config.yml"),
		filepath.Join(base, "config.json"),
	}
	var data map[string]any
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.HasSuffix(p, ".json") {
			if m, err := parseJSON(b); err == nil {
				data = m
				break
			}
		} else {
			if m, err := parseYAMLShallow(string(b)); err == nil {
				data = m
				break
			}
		}
	}
	if len(data) == 0 {
		return nil
	}
	// Apply to env if not set already
	for _, key := range KnownKeys {
		if os.Getenv(key) != "" {
			continue
		}
		if v, ok := lookupInsensitive(data, key); ok {
			os.Setenv(key, toString(v))
		}
	}
	return nil
}

func parseJSON(b []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// parseYAMLShallow parses very shallow YAML with top-level key: value pairs.
// It ignores nested objects/arrays and comments. Values can be quoted strings,
// booleans, or numbers; everything else is treated as string.
func parseYAMLShallow(s string) (map[string]any, error) {
	m := make(map[string]any)
	rd := bufio.NewScanner(strings.NewReader(s))
	lineNum := 0
	for rd.Scan() {
		lineNum++
		line := strings.TrimSpace(rd.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// skip indented (nested) lines
		if strings.HasPrefix(rd.Text(), " ") || strings.HasPrefix(rd.Text(), "\t") {
			continue
		}
		// split on the first ':'
		i := strings.IndexRune(line, ':')
		if i <= 0 {
			// not a k:v line; ignore
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		// remove inline comment
		if j := strings.Index(val, " #"); j >= 0 {
			val = strings.TrimSpace(val[:j])
		}
		// unquote if quoted
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			val = strings.TrimSuffix(strings.TrimPrefix(val, string(val[0])), string(val[len(val)-1]))
		}
		// try bool/number
		if b, err := strconv.ParseBool(strings.ToLower(val)); err == nil {
			m[key] = b
			continue
		}
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			m[key] = n
			continue
		}
		m[key] = val
	}
	if err := rd.Err(); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, errors.New("empty or unsupported YAML")
	}
	return m, nil
}

func lookupInsensitive(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	// allow lower/upper keys
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		// avoid trailing .0 for integer-like values
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
