package indexer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type FileDoc struct {
	Path    string
	Content string
	SHA     string
	Lang    string
	MTime   string
}

type Options struct {
	MaxFiles    int
	MaxFileSize int64    // bytes
	Include     []string // glob patterns relative to root
	Exclude     []string // glob patterns relative to root
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

	// Prefer git-aware listing (respects .gitignore), fallback to WalkDir
	files := make([]string, 0, opt.MaxFiles)
	if useGitListing(root) {
		if lst, err := gitListFiles(root); err == nil && len(lst) > 0 {
			files = lst
		}
	}
	if len(files) == 0 {
		files = walkListFiles(root, opt.MaxFiles)
	}

	var docs []FileDoc
	for _, path := range files {
		if len(docs) >= opt.MaxFiles {
			break
		}
		if isDenied(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() > opt.MaxFileSize {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil || looksBinary(b) {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if len(opt.Include) > 0 && !matchAny(rel, opt.Include) {
			continue
		}
		if len(opt.Exclude) > 0 && matchAny(rel, opt.Exclude) {
			continue
		}
		docs = append(docs, FileDoc{
			Path:    rel,
			Content: string(b),
			SHA:     sha256Hex(b),
			Lang:    detectLang(path),
			MTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return docs, nil
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

func matchAny(rel string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
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

// useGitListing reports whether to try git-based file listing.
func useGitListing(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	return false
}

// gitListFiles returns tracked and untracked (not ignored) files using .gitignore rules.
func gitListFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	// split by NUL
	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		abs := filepath.Join(root, string(p))
		files = append(files, abs)
	}
	return files, nil
}

// walkListFiles walks root and returns non-dir paths with basic dir skips.
func walkListFiles(root string, max int) []string {
	files := make([]string, 0, max)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := defaultSkips[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		if len(files) >= max {
			return fs.SkipAll
		}
		return nil
	})
	return files
}
