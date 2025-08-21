package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

var levelNames = map[Level]string{Debug: "debug", Info: "info", Warn: "warn", Error: "error"}
var nameToLevel = map[string]Level{"debug": Debug, "info": Info, "warn": Warn, "error": Error}

type Logger struct {
	out    io.Writer
	level  Level
	fields map[string]string
	mu     sync.Mutex
}

func New() *Logger {
	lvl := Info
	if v := strings.ToLower(os.Getenv("MYCODER_LOG_LEVEL")); v != "" {
		if l, ok := nameToLevel[v]; ok {
			lvl = l
		}
	}
	return &Logger{out: os.Stderr, level: lvl, fields: make(map[string]string)}
}

func (l *Logger) With(kv map[string]string) *Logger {
	child := &Logger{out: l.out, level: l.level, fields: make(map[string]string)}
	for k, v := range l.fields {
		child.fields[k] = v
	}
	for k, v := range kv {
		child.fields[k] = v
	}
	return child
}

func (l *Logger) write(level Level, msg string, kv map[string]any) {
	if level < l.level {
		return
	}
	rec := make(map[string]any, 4+len(l.fields)+(len(kv)))
	rec["ts"] = time.Now().Format(time.RFC3339)
	rec["level"] = levelNames[level]
	rec["msg"] = msg
	for k, v := range l.fields {
		rec[k] = v
	}
	for k, v := range kv {
		rec[k] = v
	}
	maskSecrets(rec)
	l.mu.Lock()
	defer l.mu.Unlock()
	b, _ := json.Marshal(rec)
	_, _ = l.out.Write(append(b, '\n'))
}

func (l *Logger) Debug(msg string, kv ...any) { l.write(Debug, msg, toMap(kv...)) }
func (l *Logger) Info(msg string, kv ...any)  { l.write(Info, msg, toMap(kv...)) }
func (l *Logger) Warn(msg string, kv ...any)  { l.write(Warn, msg, toMap(kv...)) }
func (l *Logger) Error(msg string, kv ...any) { l.write(Error, msg, toMap(kv...)) }

func toMap(kv ...any) map[string]any {
	m := make(map[string]any)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		m[k] = kv[i+1]
	}
	return m
}

// maskSecrets redacts likely secret values in-place.
func maskSecrets(m map[string]any) {
	secretKeys := []string{"key", "token", "secret", "password", "authorization", "api_key", "apikey", "bearer"}
	for k, v := range m {
		if s, ok := v.(string); ok {
			lowerK := strings.ToLower(k)
			for _, p := range secretKeys {
				if strings.Contains(lowerK, p) {
					m[k] = redact(s)
					goto next
				}
			}
			// redact bearer tokens in values
			if strings.HasPrefix(strings.ToLower(s), "bearer ") {
				parts := strings.SplitN(s, " ", 2)
				if len(parts) == 2 {
					m[k] = "Bearer " + redact(parts[1])
				}
				goto next
			}
			// common provider secret prefixes
			if strings.HasPrefix(s, "sk-") || looksSecret(s) {
				m[k] = redact(s)
				goto next
			}
		}
	next:
	}
}

var secretLike = regexp.MustCompile(`(?i)[a-z0-9_\-]{20,}`)

func looksSecret(s string) bool { return secretLike.MatchString(s) }

func redact(s string) string {
	n := len(s)
	if n <= 8 {
		return "***"
	}
	head, tail := s[:4], s[n-4:]
	return fmt.Sprintf("%s***%s", head, tail)
}
