package indexer

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileDoc struct {
	Path    string
	Content string
	SHA     string
	Lang    string
}

type Options struct {
	MaxFiles    int
	MaxFileSize int64 // bytes
}

var defaultSkips = map[string]struct{}{
	".git": {}, "node_modules": {}, "vendor": {}, "dist": {}, "build": {}, ".next": {}, ".cache": {},
}

var extDeny = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".ico": {},
	".pdf": {}, ".zip": {}, ".gz": {}, ".tar": {}, ".xz": {}, ".7z": {}, ".mp4": {}, ".mov": {}, ".mp3": {},
}

// Index walks root and returns text file contents up to limits.
func Index(root string, opt Options) ([]FileDoc, error) {
	if opt.MaxFiles <= 0 {
		opt.MaxFiles = 500
	}
	if opt.MaxFileSize <= 0 {
		opt.MaxFileSize = 256 * 1024 // 256KB
	}

	var docs []FileDoc
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		} // skip errors
		name := d.Name()
		if d.IsDir() {
			if _, skip := defaultSkips[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if len(docs) >= opt.MaxFiles {
			return fs.SkipAll
		}
		if isDenied(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > opt.MaxFileSize {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if looksBinary(b) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		docs = append(docs, FileDoc{
			Path:    filepath.ToSlash(rel),
			Content: string(b),
			SHA:     sha256Hex(b),
			Lang:    detectLang(path),
		})
		return nil
	})
	return docs, err
}

func isDenied(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, deny := extDeny[ext]
	return deny
}

func looksBinary(b []byte) bool {
	// Heuristic: reject if contains NUL byte in first 8000 bytes
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func detectLang(path string) string {
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
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
