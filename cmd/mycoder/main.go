package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"mycoder/internal/config"
	mylog "mycoder/internal/log"
	"mycoder/internal/server"
	"mycoder/internal/version"
)

func main() {
	// load config file and apply env (env has precedence)
	_ = config.LoadAndApply()
	if len(os.Args) < 2 {
		// No arguments provided - start interactive chat mode
		interactiveChatMode()
		return
	}

	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", ":8089", "listen address")
		_ = fs.Parse(os.Args[2:])
		// structured startup log
		{
			lg := mylog.New()
			lg.Info("server.start", "addr", *addr)
		}
		if err := server.Run(*addr); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(version.String())
	case "projects":
		projectsCmd(os.Args[2:])
	case "index":
		indexCmd(os.Args[2:])
	case "search":
		searchCmd(os.Args[2:])
	case "ask":
		askCmd(os.Args[2:])
	case "chat":
		chatCmd(os.Args[2:])
	case "models":
		modelsCmd(os.Args[2:])
	case "metrics":
		metricsCmd(os.Args[2:])
	case "explain":
		explainCmd(os.Args[2:])
	case "edit":
		editCmd(os.Args[2:])
	case "hooks":
		hooksCmd(os.Args[2:])
	case "test":
		testCmd(os.Args[2:])
	case "exec":
		execCmd(os.Args[2:])
	case "knowledge":
		knowledgeCmd(os.Args[2:])
	case "fs":
		fsCmd(os.Args[2:])
	case "mcp":
		mcpCmd(os.Args[2:])
	case "seed":
		seedCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()

	default:
		// basic placeholders for planned commands
		cmd := os.Args[1]
		if isKnownStub(cmd) {
			fmt.Printf("%s: not implemented yet\n", cmd)
			os.Exit(0)
		}
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("mycoder - project-aware coding CLI")
	fmt.Println("usage:")
	fmt.Println("  mycoder                           - Interactive chat mode (like Claude Code)")
	fmt.Println("  mycoder serve [--addr :8089]")
	fmt.Println("  mycoder version")
	fmt.Println("  mycoder projects [list|create]")
	fmt.Println("  mycoder index --project <id> [--mode full|incremental]")
	fmt.Println("  mycoder search \"<query>\" [--project <id>]")
	fmt.Println("  mycoder ask [--project <id>] [--k 5] \"<question>\"")
	fmt.Println("  mycoder chat [--project <id>] [--k 5] \"<prompt>\"")
	fmt.Println("  mycoder models")
	fmt.Println("  mycoder metrics")
	fmt.Println("  mycoder knowledge [add|list|vet|promote|reverify|gc]")
	fmt.Println("  mycoder fs [read|write|delete|patch] --project <id> --path <p> [--content ...] [--start N --length N --replace ...]")
	fmt.Println("  mycoder fs diff --project <id> --path <p> --new-file <file> [--context 3] [--ignore-crlf] [--color]")
	fmt.Println("  mycoder fs patch-unified --project <id> --file <diff.patch> [--dry-run|--yes] [--color]")
	fmt.Println("  mycoder fs patch-unified-rollback --project <id> --patch-id <id> [--dry-run|--yes]")
	fmt.Println("  mycoder exec -- -- <cmd> [args...]")
	fmt.Println("  mycoder explain --project <id> <path|symbol>")
	fmt.Println("  mycoder edit --project <id> --goal \"<설명>\" [--files a.go,b.go] [--stream]")
	fmt.Println("  mycoder mcp tools|call --name <tool> --json '<params>'")
	fmt.Println("  mycoder test --project <id> [--timeout 60] [--verbose]")
	fmt.Println("  mycoder seed rag --project <id> [--docs] [--code] [--web-json <file>] [--dry-run] [--pin]")
	fmt.Println("  mycoder <command> (coming soon): edit | hooks | fs | exec | mcp")
}

func isKnownStub(cmd string) bool {
	known := []string{"ask", "chat", "hooks", "fs", "exec"}
	for _, k := range known {
		if strings.EqualFold(k, cmd) {
			return true
		}
	}
	return false
}

func serverURL() string {
	if v := os.Getenv("MYCODER_SERVER_URL"); v != "" {
		return v
	}
	return "http://localhost:8089"
}

func projectsCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mycoder projects [list|create]")
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		resp, err := http.Get(serverURL() + "/projects")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "create":
		fs := flag.NewFlagSet("projects create", flag.ExitOnError)
		name := fs.String("name", "", "project name")
		root := fs.String("root", ".", "project root path")
		_ = fs.Parse(args[1:])
		if *name == "" || *root == "" {
			fmt.Println("--name and --root required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"name":"%s","rootPath":"%s"}`, *name, *root)
		resp, err := http.Post(serverURL()+"/projects", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	default:
		fmt.Println("usage: mycoder projects [list|create]")
		os.Exit(1)
	}
}

func indexCmd(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	mode := fs.String("mode", "full", "full|incremental")
	stream := fs.Bool("stream", false, "stream progress (SSE)")
	retries := fs.Int("retries", 0, "auto-retry times on stream error")
	save := fs.String("save-log", "", "save stream lines to file")
	maxFiles := fs.Int("max-files", 0, "max files to index")
	maxBytes := fs.Int("max-bytes", 0, "max file size bytes")
	include := fs.String("include", "", "comma-separated glob patterns to include")
	exclude := fs.String("exclude", "", "comma-separated glob patterns to exclude")
	_ = fs.Parse(args)
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}
	body := fmt.Sprintf(`{"projectID":"%s","mode":"%s","maxFiles":%d,"maxBytes":%d,"include":[%s],"exclude":[%s]}`,
		*project, *mode, *maxFiles, *maxBytes, toJSONStringArray(*include), toJSONStringArray(*exclude))
	if *stream {
		attempts := *retries + 1
		for i := 0; i < attempts; i++ {
			ctx, cancel := signalContext()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL()+"/index/run/stream", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				cancel()
				if i == attempts-1 {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				continue
			}
			rd := bufio.NewScanner(resp.Body)
			lastEvent := ""
			total, indexed := 0, 0
			var jobID string
			for rd.Scan() {
				line := rd.Text()
				if strings.HasPrefix(line, "event:") {
					lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
					continue
				}
				if strings.HasPrefix(line, "data:") {
					data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if *save != "" {
						_ = appendLog(*save, fmt.Sprintf("%s %s\n", lastEvent, data))
					}
					switch lastEvent {
					case "job":
						jobID = data
						fmt.Printf("job: %s\n", jobID)
					case "progress":
						var p struct{ Indexed, Total int }
						_ = json.Unmarshal([]byte(data), &p)
						total, indexed = p.Total, p.Indexed
						fmt.Printf("progress: %d/%d\n", indexed, total)
					case "completed":
						fmt.Println("completed")
					case "error":
						fmt.Fprintln(os.Stderr, data)
					}
				}
			}
			resp.Body.Close()
			cancel()
			if err := rd.Err(); err != nil && i < attempts-1 {
				fmt.Fprintf(os.Stderr, "[streaming] error: %v (retrying)\n", err)
				continue
			}
			break
		}
		return
	}
	resp, err := http.Post(serverURL()+"/index/run", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
}

func searchCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mycoder search \"<query>\" [--project <id>]")
		os.Exit(1)
	}
	query := args[0]
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	_ = fs.Parse(args[1:])
	url := serverURL() + "/search?q=" + urlQueryEscape(query)
	if *project != "" {
		url += "&projectID=" + urlQueryEscape(*project)
	}
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res struct {
		Results []struct {
			Path      string  `json:"path"`
			Score     float64 `json:"score"`
			Preview   string  `json:"preview"`
			StartLine int     `json:"startLine"`
			EndLine   int     `json:"endLine"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		// fallback raw
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	for _, r := range res.Results {
		loc := r.Path
		if r.StartLine > 0 {
			if r.EndLine > 0 && r.EndLine != r.StartLine {
				loc = fmt.Sprintf("%s:%d-%d", r.Path, r.StartLine, r.EndLine)
			} else {
				loc = fmt.Sprintf("%s:%d", r.Path, r.StartLine)
			}
		}
		fmt.Printf("%s  score=%.3f\n  %s\n", loc, r.Score, r.Preview)
	}
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer(" ", "+")
	return r.Replace(s)
}

func askCmd(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	k := fs.Int("k", 5, "retrieval top K")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Println("usage: mycoder ask [--project <id>] [--k 5] \"<question>\"")
		os.Exit(1)
	}
	q := strings.Join(rest, " ")
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":false,"projectID":"%s","retrieval":{"k":%d}}`, q, *project, *k)
	resp, err := http.Post(serverURL()+"/chat", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	fmt.Println(res.Content)
}

func chatCmd(args []string) {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	k := fs.Int("k", 5, "retrieval top K")
	retries := fs.Int("retries", 0, "auto-retry times on stream error")
	tty := fs.Bool("tty", false, "print lightweight stream status to stderr")
	save := fs.String("save-log", "", "save stream lines to file")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Println("usage: mycoder chat [--project <id>] [--k 5] [--retries 0] [--tty] \"<prompt>\"")
		os.Exit(1)
	}
	q := strings.Join(rest, " ")
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":true,"projectID":"%s","retrieval":{"k":%d}}`, q, *project, *k)
	attempts := *retries + 1
	for i := 0; i < attempts; i++ {
		if *tty {
			if i == 0 {
				fmt.Fprintln(os.Stderr, "[streaming] starting...")
			} else {
				fmt.Fprintf(os.Stderr, "[streaming] retry %d/%d...\n", i, *retries)
			}
		}
		ctx, cancel := signalContext()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL()+"/chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			if i == attempts-1 {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			continue
		}
		rd := bufio.NewScanner(resp.Body)
		lastEvent := ""
		for rd.Scan() {
			line := rd.Text()
			if strings.HasPrefix(line, "event:") {
				lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if *save != "" {
					_ = appendLog(*save, fmt.Sprintf("%s %s\n", lastEvent, data))
				}
				switch lastEvent {
				case "token":
					fmt.Print(data)
				case "error":
					if data != "" {
						fmt.Fprintln(os.Stderr, data)
					}
				case "done":
					fmt.Println()
					resp.Body.Close()
					cancel()
					return
				default:
					// fallback: print raw data lines
					fmt.Print(data)
				}
			}
		}
		// if stream closed without explicit done, decide retry
		resp.Body.Close()
		cancel()
		if err := rd.Err(); err != nil && i < attempts-1 {
			if *tty {
				fmt.Fprintf(os.Stderr, "[streaming] error: %v (retrying)\n", err)
			}
			continue
		}
		// closed gracefully without done: break
		fmt.Println()
		break
	}
}

// appendLog appends a line to a file, creating it if needed.
func appendLog(path, s string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}
func modelsCmd(args []string) {
	fs := flag.NewFlagSet("models", flag.ExitOnError)
	format := fs.String("format", "table", "output format: table|json|raw")
	filter := fs.String("filter", "", "substring filter for model id")
	color := fs.Bool("color", false, "enable ANSI colors for table")
	_ = fs.Parse(args)
	base := os.Getenv("MYCODER_OPENAI_BASE_URL")
	if base == "" {
		base = "http://210.126.109.57:3620/v1"
	}
	url := strings.TrimRight(base, "/") + "/models"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if key := os.Getenv("MYCODER_OPENAI_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if *format == "raw" {
		io.Copy(os.Stdout, resp.Body)
		return
	}
	var obj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		// fallback raw
		fmt.Fprintln(os.Stderr, "warning: non-JSON response; printing raw")
		req2, _ := http.NewRequest(http.MethodGet, url, nil)
		if key := os.Getenv("MYCODER_OPENAI_API_KEY"); key != "" {
			req2.Header.Set("Authorization", "Bearer "+key)
		}
		resp2, err2 := http.DefaultClient.Do(req2)
		if err2 == nil {
			defer resp2.Body.Close()
			io.Copy(os.Stdout, resp2.Body)
		} else {
			fmt.Fprintln(os.Stderr, err2)
		}
		return
	}
	// collect model ids from OpenAI-like schema {data:[{id:..}]}
	ids := make([]string, 0)
	if v, ok := obj["data"]; ok {
		if arr, ok := v.([]any); ok {
			for _, it := range arr {
				if m, ok := it.(map[string]any); ok {
					if id, ok := m["id"].(string); ok {
						ids = append(ids, id)
					}
				}
			}
		}
	}
	sort.Strings(ids)
	if *filter != "" {
		f := strings.ToLower(*filter)
		keep := ids[:0]
		for _, id := range ids {
			if strings.Contains(strings.ToLower(id), f) {
				keep = append(keep, id)
			}
		}
		ids = keep
	}
	switch *format {
	case "json":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"models": ids})
	default: // table
		for _, id := range ids {
			if *color {
				fmt.Println(colorCyan(id))
			} else {
				fmt.Println(id)
			}
		}
	}
}

func metricsCmd(args []string) {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "fetch and pretty-print JSON")
	color := fs.Bool("color", false, "colorize keys (text mode)")
	_ = fs.Parse(args)
	url := serverURL() + "/metrics"
	if *asJSON {
		url += "?format=json"
	}
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if !*asJSON {
		// text mode: passthrough
		if *color {
			// naive colorization: highlight metric names at line start
			rd := bufio.NewScanner(resp.Body)
			for rd.Scan() {
				line := rd.Text()
				if len(line) > 0 && line[0] != '#' && strings.Contains(line, " ") {
					parts := strings.SplitN(line, " ", 2)
					fmt.Println(colorCyan(parts[0]) + " " + parts[1])
				} else {
					fmt.Println(line)
				}
			}
			return
		}
		io.Copy(os.Stdout, resp.Body)
		return
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	// stable key order
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			if *color {
				fmt.Printf("%s: %.0f\n", colorCyan(k), v)
			} else {
				fmt.Printf("%s: %.0f\n", k, v)
			}
		default:
			b, _ := json.Marshal(v)
			if *color {
				fmt.Printf("%s: %s\n", colorCyan(k), string(b))
			} else {
				fmt.Printf("%s: %s\n", k, string(b))
			}
		}
	}
}

func knowledgeCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mycoder knowledge [add|list|vet] ...")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("knowledge add", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		typ := fs.String("type", "doc", "code|doc|web")
		title := fs.String("title", "", "title")
		url := fs.String("url", "", "path or URL")
		text := fs.String("text", "", "content text")
		trust := fs.Float64("trust", 0.0, "initial trust score")
		pinned := fs.Bool("pin", false, "pin this knowledge")
		_ = fs.Parse(args[1:])
		if *project == "" || *text == "" {
			fmt.Println("--project and --text required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","sourceType":"%s","pathOrURL":"%s","title":"%s","text":%q,"trustScore":%f,"pinned":%v}`,
			*project, *typ, *url, *title, *text, *trust, *pinned)
		resp, err := http.Post(serverURL()+"/knowledge", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "list":
		fs := flag.NewFlagSet("knowledge list", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		_ = fs.Parse(args[1:])
		if *project == "" {
			fmt.Println("--project required")
			os.Exit(1)
		}
		url := serverURL() + "/knowledge?projectID=" + urlQueryEscape(*project)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "vet":
		fs := flag.NewFlagSet("knowledge vet", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		_ = fs.Parse(args[1:])
		if *project == "" {
			fmt.Println("--project required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s"}`, *project)
		resp, err := http.Post(serverURL()+"/knowledge/vet", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "promote":
		fs := flag.NewFlagSet("knowledge promote", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		title := fs.String("title", "", "title")
		url := fs.String("url", "", "path or URL")
		text := fs.String("text", "", "content text")
		commit := fs.String("commit", "", "commit SHA")
		files := fs.String("files", "", "files csv")
		symbols := fs.String("symbols", "", "symbols csv")
		pin := fs.Bool("pin", false, "pin this knowledge")
		_ = fs.Parse(args[1:])
		if *project == "" || *text == "" {
			fmt.Println("--project and --text required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","title":"%s","text":%q,"pathOrURL":"%s","commitSHA":"%s","files":"%s","symbols":"%s","pin":%v}`,
			*project, *title, *text, *url, *commit, *files, *symbols, *pin)
		resp, err := http.Post(serverURL()+"/knowledge/promote", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "reverify":
		fs := flag.NewFlagSet("knowledge reverify", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		_ = fs.Parse(args[1:])
		if *project == "" {
			fmt.Println("--project required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s"}`, *project)
		resp, err := http.Post(serverURL()+"/knowledge/reverify", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "promote-auto":
		fs := flag.NewFlagSet("knowledge promote-auto", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		title := fs.String("title", "", "title")
		files := fs.String("files", "", "comma-separated file paths")
		pin := fs.Bool("pin", false, "pin this knowledge")
		_ = fs.Parse(args[1:])
		if *project == "" || *files == "" {
			fmt.Println("--project and --files required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","title":"%s","files":[%s],"pin":%v}`,
			*project, *title, toJSONStringArray(*files), *pin)
		resp, err := http.Post(serverURL()+"/knowledge/promote/auto", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "gc":
		fs := flag.NewFlagSet("knowledge gc", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		min := fs.Float64("min", 0.5, "min trust score")
		_ = fs.Parse(args[1:])
		if *project == "" {
			fmt.Println("--project required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","Min":%f}`, *project, *min)
		resp, err := http.Post(serverURL()+"/knowledge/gc", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()

	case "approve":
		fs := flag.NewFlagSet("knowledge approve", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		ids := fs.String("ids", "", "comma-separated knowledge IDs")
		min := fs.Float64("min", 0.8, "min trust score after approve")
		pin := fs.Bool("pin", true, "pin items on approve")
		_ = fs.Parse(args[1:])
		if *project == "" || *ids == "" {
			fmt.Println("--project and --ids required")
			os.Exit(1)
		}
		var b strings.Builder
		b.WriteString(`{"ProjectID":"` + *project + `","IDs":[`)
		parts := strings.Split(*ids, ",")
		for i, id := range parts {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(fmt.Sprintf("%q", strings.TrimSpace(id)))
		}
		b.WriteString(fmt.Sprintf(`],"Pin":%v,"MinTrust":%f}`, *pin, *min))
		resp, err := http.Post(serverURL()+"/knowledge/approve", "application/json", strings.NewReader(b.String()))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
		io.Copy(os.Stdout, resp.Body)
	default:
		fmt.Println("usage: mycoder knowledge [add|list|vet] ...")
		os.Exit(1)
	}
}

func toJSONStringArray(csv string) string {
	parts := strings.Split(csv, ",")
	for i := range parts {
		parts[i] = fmt.Sprintf("%q", strings.TrimSpace(parts[i]))
	}
	return strings.Join(parts, ",")
}

func parseEnvCSV(csv string) map[string]string {
	m := make(map[string]string)
	if strings.TrimSpace(csv) == "" {
		return m
	}
	parts := strings.Split(csv, ",")
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}

// seedCmd implements: mycoder seed rag --project <id> [--docs] [--code] [--web-json <file>] [--dry-run] [--pin]
func seedCmd(args []string) {
	if len(args) == 0 || args[0] != "rag" {
		fmt.Println("usage: mycoder seed rag --project <id> [--docs] [--code] [--web-json <file>] [--dry-run] [--pin]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("seed rag", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	includeDocs := fs.Bool("docs", true, "seed internal docs")
	includeCode := fs.Bool("code", true, "seed code summaries")
	webJSON := fs.String("web-json", "", "path to JSON file for web references (optional)")
	dry := fs.Bool("dry-run", false, "print actions but do not POST")
	pin := fs.Bool("pin", true, "pin knowledge items when applicable")
	_ = fs.Parse(args[1:])
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}

	// internal docs seeds (title -> csv files)
	docSeeds := []struct{ title, files string }{
		{"PRD", "docs/PRD.md"},
		{"Architecture", "docs/ARCHITECTURE.md"},
		{"API", "docs/API.md"},
		{"Data Model", "docs/DATA_MODEL.md"},
		{"RAG", "docs/RAG.md,docs/MEMORY.md"},
		{"LLM", "docs/LLM.md"},
		{"CLI/Tools", "docs/CLI_UX.md,docs/TOOLS.md"},
		{"Testing/CI", "docs/TESTING_CI.md"},
		{"Roadmap", "docs/ROADMAP.md"},
	}
	// code summary seeds
	codeSeeds := []struct{ title, files string }{
		{"Server Overview", "internal/server/server.go"},
		{"Indexer", "internal/indexer/indexer.go"},
		{"Retriever", "internal/rag/retriever/knn.go,internal/rag/retriever/bm25.go,internal/rag/retriever/hybrid.go"},
		{"Patch Utilities", "internal/patch/unified.go,internal/patch/apply.go"},
		{"CLI Entrypoint", "cmd/mycoder/main.go"},
	}

	// run promote-auto for each seed
	runPromote := func(title, files string) error {
		body := fmt.Sprintf(`{"projectID":"%s","title":"%s","files":[%s],"pin":%v}`,
			*project, title, toJSONStringArray(files), *pin)
		if *dry {
			fmt.Printf("[dry-run] promote-auto: %s <- [%s]\n", title, files)
			return nil
		}
		resp, err := http.Post(serverURL()+"/knowledge/promote/auto", "application/json", strings.NewReader(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("promote-auto failed: %s", resp.Status)
		}
		fmt.Printf("seeded: %s\n", title)
		return nil
	}

	if *includeDocs {
		for _, s := range docSeeds {
			if err := runPromote(s.title, s.files); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
	if *includeCode {
		for _, s := range codeSeeds {
			if err := runPromote(s.title, s.files); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}

	// optional web ingest
	if strings.TrimSpace(*webJSON) != "" {
		b, err := os.ReadFile(*webJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// ensure projectID presence; if not present, wrap
		payload := string(b)
		if !strings.Contains(payload, "\"projectID\"") {
			payload = fmt.Sprintf(`{"projectID":"%s","results":%s,"dedupe":true}`, *project, strings.TrimSpace(string(b)))
		}
		if *dry {
			fmt.Printf("[dry-run] web ingest from %s\n", *webJSON)
			return
		}
		resp, err := http.Post(serverURL()+"/web/ingest", "application/json", strings.NewReader(payload))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	}
}

func tailLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	// preserve trailing newline behavior
	tail := lines[len(lines)-n:]
	return strings.Join(tail, "\n")
}

func tailBytes(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func colorRed(s string) string    { return "\x1b[31m" + s + "\x1b[0m" }
func colorGreen(s string) string  { return "\x1b[32m" + s + "\x1b[0m" }
func colorYellow(s string) string { return "\x1b[33m" + s + "\x1b[0m" }
func colorCyan(s string) string   { return "\x1b[36m" + s + "\x1b[0m" }

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigc; cancel() }()
	return ctx, cancel
}

func fsCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mycoder fs [read|write|delete|patch] --project <id> --path <p> [--content ...] [--start N --length N --replace ...]")
		os.Exit(1)
	}
	sub := args[0]
	switch sub {
	case "read":
		fs := flag.NewFlagSet("fs read", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		path := fs.String("path", "", "path")
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","path":"%s"}`, *project, *path)
		resp, err := http.Post(serverURL()+"/fs/read", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "write":
		fs := flag.NewFlagSet("fs write", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		path := fs.String("path", "", "path")
		content := fs.String("content", "", "content")
		dryRun := fs.Bool("dry-run", false, "print what would change and exit")
		yes := fs.Bool("yes", false, "apply without prompt (required unless --dry-run)")
		allowLarge := fs.Bool("allow-large", false, "allow large writes overriding threshold")
		largeThresh := fs.Int("large-threshold-bytes", 65536, "threshold in bytes to treat as large change")
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("[dry-run] write %s (len=%d)\n", *path, len(*content))
			return
		}
		if !*allowLarge && len(*content) > *largeThresh {
			fmt.Printf("refusing large write (%d bytes > %d). Use --allow-large to proceed or --dry-run to preview.\n", len(*content), *largeThresh)
			os.Exit(1)
		}
		if !*yes {
			fmt.Println("confirmation required: pass --yes to apply or use --dry-run")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","path":"%s","content":%q}`, *project, *path, *content)
		resp, err := http.Post(serverURL()+"/fs/write", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "delete":
		fs := flag.NewFlagSet("fs delete", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		path := fs.String("path", "", "path")
		dryRun := fs.Bool("dry-run", false, "print what would change and exit")
		yes := fs.Bool("yes", false, "apply without prompt (required unless --dry-run)")
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("[dry-run] delete %s\n", *path)
			return
		}
		if !*yes {
			fmt.Println("confirmation required: pass --yes to apply or use --dry-run")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","path":"%s"}`, *project, *path)
		resp, err := http.Post(serverURL()+"/fs/delete", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "patch":
		fs := flag.NewFlagSet("fs patch", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		path := fs.String("path", "", "path")
		start := fs.Int("start", 0, "byte start")
		length := fs.Int("length", 0, "byte length")
		replace := fs.String("replace", "", "replacement text")
		dryRun := fs.Bool("dry-run", false, "print what would change and exit")
		yes := fs.Bool("yes", false, "apply without prompt (required unless --dry-run)")
		allowLarge := fs.Bool("allow-large", false, "allow large patches overriding threshold")
		largeThresh := fs.Int("large-threshold-bytes", 65536, "threshold in bytes to treat as large change")
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("[dry-run] patch %s start=%d length=%d replace_len=%d\n", *path, *start, *length, len(*replace))
			return
		}
		change := *length
		if len(*replace) > change {
			change = len(*replace)
		}
		if !*allowLarge && change > *largeThresh {
			fmt.Printf("refusing large patch (max(change_len,replace_len)=%d > %d). Use --allow-large to proceed or --dry-run to preview.\n", change, *largeThresh)
			os.Exit(1)
		}
		if !*yes {
			fmt.Println("confirmation required: pass --yes to apply or use --dry-run")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","path":"%s","hunks":[{"start":%d,"length":%d,"replace":%q}]}`, *project, *path, *start, *length, *replace)
		resp, err := http.Post(serverURL()+"/fs/patch", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "patch-unified":
		fs := flag.NewFlagSet("fs patch-unified", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		file := fs.String("file", "", "unified diff file path")
		dryRun := fs.Bool("dry-run", false, "dry run (preview only)")
		yes := fs.Bool("yes", false, "apply without prompt (required unless --dry-run)")
		ignoreWS := fs.Bool("ignore-ws", false, "ignore whitespace when applying (fuzzy)")
		color := fs.Bool("color", false, "colorize diff summary")
		_ = fs.Parse(args[1:])
		if *project == "" || *file == "" {
			fmt.Println("--project and --file required")
			os.Exit(1)
		}
		b, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","diffText":%q,"dryRun":%v,"yes":%v}`, *project, string(b), *dryRun, *yes)
		url := serverURL() + "/fs/patch/unified"
		if *ignoreWS {
			url += "?ignorews=1"
		}
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		var res struct {
			Ok           bool   `json:"ok"`
			DryRun       bool   `json:"dryRun"`
			PatchID      string `json:"patchID"`
			TotalAdd     int    `json:"totalAdd"`
			TotalDel     int    `json:"totalDel"`
			WrittenBytes int    `json:"writtenBytes"`
			Files        []struct {
				Path                   string
				Add, Del, WrittenBytes int
				Conflict               string
			}
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			_, _ = io.Copy(os.Stdout, resp.Body)
			return
		}
		if *color {
			fmt.Printf("%s +%d %s -%d\n", colorGreen("added"), res.TotalAdd, colorRed("deleted"), res.TotalDel)
		} else {
			fmt.Printf("added +%d deleted -%d\n", res.TotalAdd, res.TotalDel)
		}
		for _, f := range res.Files {
			name := f.Path
			if *color {
				if f.Conflict != "" {
					name = colorRed(name)
				} else if f.WrittenBytes > 0 {
					name = colorGreen(name)
				} else {
					name = colorCyan(name)
				}
			}
			fmt.Printf("  %s (+%d/-%d)", name, f.Add, f.Del)
			if f.WrittenBytes > 0 {
				fmt.Printf(" [%dB]", f.WrittenBytes)
			}
			if f.Conflict != "" {
				fmt.Printf(" conflict: %s", f.Conflict)
			}
			fmt.Println()
		}
		if res.PatchID != "" {
			fmt.Printf("patchID: %s\n", res.PatchID)
		}
		if !res.Ok {
			os.Exit(1)
		}
		if res.DryRun && *color {
			fmt.Println("\nPreview:")
			// colorize full diff content
			fmt.Print(colorizeUnifiedDiff(string(b)))
		}
	case "patch-unified-rollback":
		fs := flag.NewFlagSet("fs patch-unified-rollback", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		patchID := fs.String("patch-id", "", "patch ID returned from apply")
		dryRun := fs.Bool("dry-run", false, "dry run (preview only)")
		yes := fs.Bool("yes", false, "confirm rollback")
		_ = fs.Parse(args[1:])
		if *project == "" || *patchID == "" {
			fmt.Println("--project and --patch-id required")
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","patchID":"%s","dryRun":%v,"yes":%v}`, *project, *patchID, *dryRun, *yes)
		resp, err := http.Post(serverURL()+"/fs/patch/unified/rollback", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "diff":
		fs := flag.NewFlagSet("fs diff", flag.ExitOnError)
		project := fs.String("project", "", "project ID")
		path := fs.String("path", "", "path")
		newFile := fs.String("new-file", "", "path to new content file")
		context := fs.Int("context", 3, "context lines")
		ignoreCRLF := fs.Bool("ignore-crlf", false, "ignore CRLF differences")
		color := fs.Bool("color", false, "colorize diff")
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" || *newFile == "" {
			fmt.Println("--project, --path and --new-file required")
			os.Exit(1)
		}
		b, err := os.ReadFile(*newFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		body := fmt.Sprintf(`{"projectID":"%s","path":"%s","newContent":%q,"context":%d,"ignoreCRLF":%v}`, *project, *path, string(b), *context, *ignoreCRLF)
		resp, err := http.Post(serverURL()+"/fs/diff", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		var res struct {
			Diff string `json:"diffText"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			_, _ = io.Copy(os.Stdout, resp.Body)
			return
		}
		if *color {
			fmt.Print(colorizeUnifiedDiff(res.Diff))
		} else {
			fmt.Print(res.Diff)
		}
	default:
		fmt.Println("usage: mycoder fs [read|write|delete|patch] --project <id> --path <p> [--content ...] [--start N --length N --replace ...]")
		os.Exit(1)
	}
}

func execCmd(args []string) {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	timeout := fs.Int("timeout", 30, "timeout in seconds")
	stream := fs.Bool("stream", false, "stream output (SSE)")
	cwd := fs.String("cwd", "", "working directory relative to project root")
	envCSV := fs.String("env", "", "comma-separated K=V pairs to pass (whitelist)")
	tail := fs.Int("tail", 0, "print only the last N lines (non-stream)")
	maxBytes := fs.Int("max-bytes", 0, "limit printed bytes to N (non-stream)")
	streamTail := fs.Int("stream-tail", 0, "buffer and print only last N lines at end (stream)")
	retries := fs.Int("retries", 0, "auto-retry times on stream error")
	save := fs.String("save-log", "", "save stream lines to file")
	_ = fs.Parse(args)
	rest := fs.Args()
	if *project == "" || len(rest) == 0 {
		fmt.Println("usage: mycoder exec --project <id> [--timeout 30] [--stream] -- <cmd> [args...]")
		os.Exit(1)
	}
	cmd := rest[0]
	var argv []string
	if len(rest) > 1 {
		argv = rest[1:]
	}
	// build JSON body
	body := struct {
		ProjectID string            `json:"projectID"`
		Cmd       string            `json:"cmd"`
		Args      []string          `json:"args"`
		Timeout   int               `json:"timeoutSec"`
		Cwd       string            `json:"cwd"`
		Env       map[string]string `json:"env"`
	}{ProjectID: *project, Cmd: cmd, Args: argv, Timeout: *timeout, Cwd: *cwd, Env: parseEnvCSV(*envCSV)}
	b, _ := json.Marshal(body)
	if *stream {
		attempts := *retries + 1
		for i := 0; i < attempts; i++ {
			ctx2, cancel2 := signalContext()
			req, _ := http.NewRequestWithContext(ctx2, http.MethodPost, serverURL()+"/shell/exec/stream", strings.NewReader(string(b)))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				cancel2()
				if i == attempts-1 {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				continue
			}
			rd := bufio.NewScanner(resp.Body)
			lastEvent := ""
			exitCode := 0
			limited := false
			var bufOut, bufErr []string
			push := func(buf *[]string, line string, max int) {
				if max > 0 {
					*buf = append(*buf, line)
					if len(*buf) > max {
						*buf = (*buf)[len(*buf)-max:]
					}
				}
			}
			for rd.Scan() {
				line := rd.Text()
				if strings.HasPrefix(line, "event:") {
					lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
					continue
				}
				if strings.HasPrefix(line, "data:") {
					data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if *save != "" {
						_ = appendLog(*save, fmt.Sprintf("%s %s\n", lastEvent, data))
					}
					switch lastEvent {
					case "stdout":
						if *streamTail > 0 {
							push(&bufOut, data, *streamTail)
						} else {
							fmt.Println(data)
						}
					case "stderr":
						if *streamTail > 0 {
							push(&bufErr, data, *streamTail)
						} else {
							fmt.Fprintln(os.Stderr, data)
						}
					case "exit":
						fmt.Sscanf(data, "%d", &exitCode)
					case "limit":
						limited = true
					}
				}
			}
			resp.Body.Close()
			cancel2()
			if *streamTail > 0 {
				if limited {
					fmt.Fprintln(os.Stderr, "[limit] output truncated by server")
				}
				if len(bufOut) > 0 {
					fmt.Fprintf(os.Stdout, "---- stdout (last %d lines) ----\n", len(bufOut))
					for _, l := range bufOut {
						fmt.Println(l)
					}
				}
				if len(bufErr) > 0 {
					fmt.Fprintf(os.Stderr, "---- stderr (last %d lines) ----\n", len(bufErr))
					for _, l := range bufErr {
						fmt.Fprintln(os.Stderr, l)
					}
				}
			}
			if err := rd.Err(); err != nil && i < attempts-1 {
				continue
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return
		}
	}
	resp, err := http.Post(serverURL()+"/shell/exec", "application/json", strings.NewReader(string(b)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res struct {
		ExitCode  int    `json:"exitCode"`
		Output    string `json:"output"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	out := res.Output
	if *tail > 0 {
		out = tailLines(out, *tail)
	}
	if *maxBytes > 0 {
		out = tailBytes(out, *maxBytes)
	}
	fmt.Print(out)
	if res.Truncated {
		fmt.Fprintln(os.Stderr, "[limit] output truncated by server")
	}
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
}

func hooksCmd(args []string) {
	if len(args) == 0 || args[0] != "run" {
		fmt.Println("usage: mycoder hooks run [--project <id>] [--targets fmt-check,test,lint] [--timeout 60] [--verbose] [--save <path.json>]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("hooks run", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	targets := fs.String("targets", "", "comma-separated targets (fmt-check,test,lint)")
	timeout := fs.Int("timeout", 60, "timeout in seconds per target")
	verbose := fs.Bool("verbose", false, "print each target output")
	useColor := fs.Bool("color", false, "colorize status and hints")
	save := fs.String("save", "", "save structured results JSON to project-relative path")
	_ = fs.Parse(args[1:])
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}
	extra := ""
	if strings.TrimSpace(*save) != "" {
		extra = fmt.Sprintf(`,"artifactPath":%q`, *save)
	}
	body := fmt.Sprintf(`{"projectID":"%s","targets":[%s],"timeoutSec":%d%s}`, *project, toJSONStringArray(*targets), *timeout, extra)
	resp, err := http.Post(serverURL()+"/tools/hooks", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res map[string]struct {
		Ok         bool   `json:"ok"`
		Output     string `json:"output"`
		Suggestion string `json:"suggestion"`
		DurationMs int    `json:"durationMs"`
		Lines      int    `json:"lines"`
		Bytes      int    `json:"bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		// fallback raw
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	fmt.Println("Hooks summary:")
	failed := false
	// stable order
	order := []string{"fmt-check", "test", "lint"}
	for _, k := range order {
		if v, ok := res[k]; ok {
			mark := "✅"
			if !v.Ok {
				mark = "❌"
				failed = true
			}
			// summary suffix (e.g., 12ms, 10 ln, 120 B)
			suffix := fmt.Sprintf(" (%dms, %d ln, %d B)", v.DurationMs, v.Lines, v.Bytes)
			name := k
			if *useColor {
				if v.Ok {
					name = colorGreen(name)
				} else {
					name = colorRed(name)
				}
			}
			fmt.Printf("  %s %s%s\n", mark, name, suffix)
			if v.Suggestion != "" {
				if *useColor {
					fmt.Printf("    %s %s\n", colorYellow("Hint:"), v.Suggestion)
				} else {
					fmt.Printf("    Hint: %s\n", v.Suggestion)
				}
			}
			if *verbose || !v.Ok {
				// indent output
				for _, line := range strings.Split(v.Output, "\n") {
					if strings.TrimSpace(line) == "" {
						continue
					}
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}
	// print any extra keys not in default order
	for k, v := range res {
		if k == "fmt-check" || k == "test" || k == "lint" {
			continue
		}
		mark := "✅"
		if !v.Ok {
			mark = "❌"
			failed = true
		}
		suffix := fmt.Sprintf(" (%dms, %d ln, %d B)", v.DurationMs, v.Lines, v.Bytes)
		name := k
		if *useColor {
			if v.Ok {
				name = colorGreen(name)
			} else {
				name = colorRed(name)
			}
		}
		fmt.Printf("  %s %s%s\n", mark, name, suffix)
		if v.Suggestion != "" {
			if *useColor {
				fmt.Printf("    %s %s\n", colorYellow("Hint:"), v.Suggestion)
			} else {
				fmt.Printf("    Hint: %s\n", v.Suggestion)
			}
		}
		if *verbose || !v.Ok {
			for _, line := range strings.Split(v.Output, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Printf("    %s\n", line)
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}

// testCmd runs only the test target via hooks API for convenience.
func testCmd(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	timeout := fs.Int("timeout", 60, "timeout in seconds")
	verbose := fs.Bool("verbose", false, "print test output")
	_ = fs.Parse(args)
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}
	body := fmt.Sprintf(`{"projectID":"%s","targets":["test"],"timeoutSec":%d}`, *project, *timeout)
	resp, err := http.Post(serverURL()+"/tools/hooks", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res map[string]struct {
		Ok         bool   `json:"ok"`
		Output     string `json:"output"`
		Suggestion string `json:"suggestion"`
		DurationMs int    `json:"durationMs"`
		Lines      int    `json:"lines"`
		Bytes      int    `json:"bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		// fallback raw
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	v, ok := res["test"]
	if !ok {
		fmt.Println("no test result returned")
		os.Exit(1)
	}
	mark := "✅"
	if !v.Ok {
		mark = "❌"
	}
	suffix := fmt.Sprintf(" (%dms, %d ln, %d B)", v.DurationMs, v.Lines, v.Bytes)
	fmt.Printf("%s test%s\n", mark, suffix)
	if v.Suggestion != "" {
		fmt.Printf("  Hint: %s\n", v.Suggestion)
	}
	if *verbose || !v.Ok {
		for _, line := range strings.Split(v.Output, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Printf("  %s\n", line)
		}
	}
	if !v.Ok {
		os.Exit(1)
	}
}

// explainCmd asks the model to explain a path or symbol with citations.
func explainCmd(args []string) {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	k := fs.Int("k", 7, "retrieval top K")
	stream := fs.Bool("stream", false, "stream output")
	color := fs.Bool("color", false, "colorize citations in output")
	_ = fs.Parse(args)
	rest := fs.Args()
	if *project == "" || len(rest) == 0 {
		fmt.Println("usage: mycoder explain --project <id> [--k 7] [--stream] <path|symbol>")
		os.Exit(1)
	}
	target := strings.Join(rest, " ")
	// craft prompt: instruct explanation with citations
	prompt := fmt.Sprintf("Explain '%s' in this repository. Summarize purpose, key functions, and important interactions. Cite files with line ranges.", target)
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":%v,"projectID":"%s","retrieval":{"k":%d}}`, prompt, *stream, *project, *k)
	if *stream {
		ctx, cancel := signalContext()
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL()+"/chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		rd := bufio.NewScanner(resp.Body)
		lastEvent := ""
		for rd.Scan() {
			line := rd.Text()
			if strings.HasPrefix(line, "event:") {
				lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				switch lastEvent {
				case "token":
					if *color {
						fmt.Print(highlightCitations(data))
					} else {
						fmt.Print(data)
					}
				case "error":
					if data != "" {
						fmt.Fprintln(os.Stderr, data)
					}
				case "done":
					fmt.Println()
					return
				default:
					if *color {
						fmt.Print(highlightCitations(data))
					} else {
						fmt.Print(data)
					}
				}
			}
		}
		fmt.Println()
		return
	}
	// non-streaming
	resp, err := http.Post(serverURL()+"/chat", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	fmt.Println(res.Content)
}

// editCmd requests an edit plan for the given goal and optional files.
func editCmd(args []string) {
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	goal := fs.String("goal", "", "edit goal/description")
	files := fs.String("files", "", "comma-separated files to focus on")
	k := fs.Int("k", 8, "retrieval top K")
	stream := fs.Bool("stream", false, "stream output")
	color := fs.Bool("color", false, "colorize unified diff output")
	_ = fs.Parse(args)
	if *project == "" || *goal == "" {
		fmt.Println("usage: mycoder edit --project <id> --goal \"<설명>\" [--files a.go,b.go] [--k 8] [--stream]")
		os.Exit(1)
	}
	var b strings.Builder
	b.WriteString("You are a code-edit planner. Propose a minimal, safe patch plan for the goal, with citations.\n")
	b.WriteString("Output a clear plan and suggested hunks as unified diff or patch-like blocks. Do not execute.")
	if strings.TrimSpace(*files) != "" {
		b.WriteString("\nFocus on files: ")
		b.WriteString(*files)
	}
	b.WriteString("\nGoal: ")
	b.WriteString(*goal)
	prompt := b.String()
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":%v,"projectID":"%s","retrieval":{"k":%d}}`, prompt, *stream, *project, *k)
	if *stream {
		ctx, cancel := signalContext()
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL()+"/chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		rd := bufio.NewScanner(resp.Body)
		lastEvent := ""
		for rd.Scan() {
			line := rd.Text()
			if strings.HasPrefix(line, "event:") {
				lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				switch lastEvent {
				case "token":
					fmt.Print(data)
				case "error":
					if data != "" {
						fmt.Fprintln(os.Stderr, data)
					}
				case "done":
					fmt.Println()
					return
				default:
					fmt.Print(data)
				}
			}
		}
		fmt.Println()
		return
	}
	resp, err := http.Post(serverURL()+"/chat", "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var res struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		_, _ = io.Copy(os.Stdout, resp.Body)
		return
	}
	if *color {
		fmt.Println(colorizeUnifiedDiff(res.Content))
	} else {
		fmt.Println(res.Content)
	}
}

// highlightCitations wraps path:line or path:start-end segments with cyan.
func highlightCitations(s string) string {
	parts := strings.Split(s, " ")
	for i, p := range parts {
		if strings.Count(p, ":") == 1 {
			a := strings.SplitN(p, ":", 2)
			if len(a) == 2 && a[1] != "" && isDigitsOrRange(a[1]) && looksLikePath(a[0]) {
				parts[i] = colorCyan(p)
			}
		}
	}
	return strings.Join(parts, " ")
}

func isDigitsOrRange(s string) bool {
	if s == "" {
		return false
	}
	dash := strings.IndexByte(s, '-')
	if dash < 0 {
		for i := 0; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		}
		return true
	}
	left, right := s[:dash], s[dash+1:]
	if left == "" || right == "" {
		return false
	}
	for i := 0; i < len(left); i++ {
		if left[i] < '0' || left[i] > '9' {
			return false
		}
	}
	for i := 0; i < len(right); i++ {
		if right[i] < '0' || right[i] > '9' {
			return false
		}
	}
	return true
}

func looksLikePath(s string) bool {
	return strings.ContainsAny(s, "/.") && !strings.ContainsAny(s, "\t\n\r")
}

// colorizeUnifiedDiff applies simple ANSI colors to unified diff blocks.
func colorizeUnifiedDiff(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		c := line
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff --git") {
			c = colorCyan(line)
		} else if strings.HasPrefix(line, "@@") {
			c = colorCyan(line)
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			c = colorGreen(line)
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			c = colorRed(line)
		}
		out.WriteString(c)
		out.WriteByte('\n')
	}
	return out.String()
}

// mcpCmd lists tools or calls a tool with JSON params
func mcpCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: mycoder mcp tools|call --name <tool> --json '<params>'")
		os.Exit(1)
	}
	sub := args[0]
	switch sub {
	case "tools":
		resp, err := http.Get(serverURL() + "/mcp/tools")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	case "call":
		fs := flag.NewFlagSet("mcp call", flag.ExitOnError)
		name := fs.String("name", "", "tool name")
		jsonParams := fs.String("json", "{}", "JSON params")
		_ = fs.Parse(args[1:])
		if *name == "" {
			fmt.Println("--name required")
			os.Exit(1)
		}
		// best-effort client-side schema validation
		var params map[string]any
		if err := json.Unmarshal([]byte(*jsonParams), &params); err != nil {
			fmt.Fprintln(os.Stderr, "invalid --json params:", err)
			os.Exit(1)
		}
		// fetch tools schema and validate if available
		if resp, err := http.Get(serverURL() + "/mcp/tools"); err == nil {
			defer resp.Body.Close()
			var tools struct {
				Tools []struct {
					Name         string `json:"name"`
					ParamsSchema []struct {
						Name     string `json:"name"`
						Type     string `json:"type"`
						Required bool   `json:"required"`
					} `json:"paramsSchema"`
				} `json:"tools"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&tools)
			for _, t := range tools.Tools {
				if t.Name == *name {
					for _, p := range t.ParamsSchema {
						if p.Required {
							if _, ok := params[p.Name]; !ok {
								fmt.Fprintf(os.Stderr, "missing required param: %s\n", p.Name)
								os.Exit(1)
							}
						}
						if p.Type == "string" {
							if v, ok := params[p.Name]; ok {
								if _, ok2 := v.(string); !ok2 {
									fmt.Fprintf(os.Stderr, "param %s must be string\n", p.Name)
									os.Exit(1)
								}
							}
						}
					}
					break
				}
			}
		}
		body := fmt.Sprintf(`{"name":%q,"params":%s}`, *name, *jsonParams)
		resp, err := http.Post(serverURL()+"/mcp/call", "application/json", strings.NewReader(body))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		io.Copy(os.Stdout, resp.Body)
	default:
		fmt.Println("usage: mycoder mcp tools|call --name <tool> --json '<params>'")
		os.Exit(1)
	}
}

// interactiveChatMode starts an interactive chat session similar to Claude Code or Gemini CLI
func interactiveChatMode() {
	fmt.Println("🚀 mycoder interactive chat mode")
	fmt.Println("Type your questions or commands. Use '/help' for help, '/exit' to quit.")
	fmt.Println("────────────────────────────────────────────────────────────────")

	// Check if server is running
	serverURL := getServerURL()
	if !isServerRunning(serverURL) {
		fmt.Printf("⚠️  Server not running. Starting server at %s...\n", serverURL)
		startServerInBackground()
		// Wait a bit for server to start
		fmt.Println("⏳ Waiting for server to start...")
		waitForServerReady(serverURL, 10)
	}

	// Get or create default project
	projectID := getOrCreateDefaultProject(serverURL)
	if projectID == "" {
		fmt.Println("❌ Failed to create/find default project")
		return
	}

	fmt.Printf("📁 Using project: %s\n", projectID)
	
	// Auto-index the project if needed
	fmt.Println("🔍 Checking project index status...")
	if shouldIndexProject(serverURL, projectID) {
		fmt.Println("📚 Indexing project files for better analysis...")
		go indexProjectInBackground(serverURL, projectID)
		// Don't wait for indexing to complete, let it run in background
		time.Sleep(100 * time.Millisecond) // Brief pause to let indexing start
		fmt.Println("✅ Indexing started in background")
	} else {
		fmt.Println("✅ Project index is up to date")
	}
	
	fmt.Println("────────────────────────────────────────────────────────────────")

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("💬 > ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle special commands
		switch {
		case input == "/exit" || input == "/quit" || input == "/q":
			fmt.Println("👋 Goodbye!")
			return
		case input == "/help" || input == "/h":
			printInteractiveHelp()
			continue
		case input == "/clear":
			clearScreen()
			continue
		case strings.HasPrefix(input, "/project"):
			handleProjectCommand(input, serverURL)
			continue
		case strings.HasPrefix(input, "/index"):
			handleIndexCommand(input, projectID, serverURL)
			continue
		}

		// Send chat request
		fmt.Println("🤖 Thinking...")
		response := sendChatRequest(serverURL, projectID, input)
		fmt.Println("────────────────────────────────────────────────────────────────")
		fmt.Println(response)
		fmt.Println("────────────────────────────────────────────────────────────────")
	}
}

func getServerURL() string {
	if url := os.Getenv("MYCODER_SERVER_URL"); url != "" {
		return url
	}
	return "http://localhost:8089"
}

func isServerRunning(serverURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(serverURL + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func startServerInBackground() {
	// Start server in background using exec.Command for proper process management
	cmd := exec.Command(os.Args[0], "serve", "--addr", ":8089")
	// Redirect server output to /dev/null to avoid cluttering the UI
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
	}
}

func waitForServerReady(serverURL string, maxSeconds int) {
	for i := 0; i < maxSeconds; i++ {
		if isServerRunning(serverURL) {
			fmt.Println("✅ Server is ready!")
			return
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}
	fmt.Println("\n⚠️  Server might not be ready, but continuing...")
}

func getOrCreateDefaultProject(serverURL string) string {
	// Try to list projects first
	client := &http.Client{}
	resp, err := client.Get(serverURL + "/projects")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var projects []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&projects)
		if len(projects) > 0 {
			if id, ok := projects[0]["id"].(string); ok {
				return id
			}
		}
	}

	// Create default project with mycoder_cli directory
	projectRoot := "/Users/ojinseog/myprojects/mycoder_cli"
	// Check if we're already in mycoder_cli directory
	currentDir, _ := os.Getwd()
	if strings.Contains(currentDir, "mycoder_cli") {
		projectRoot = currentDir
	}
	projectData := map[string]string{
		"name":     "mycoder_cli",
		"rootPath": projectRoot,
	}
	jsonData, _ := json.Marshal(projectData)

	resp2, err := client.Post(serverURL+"/projects", "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return ""
	}
	defer resp2.Body.Close()

	if resp2.StatusCode == 200 || resp2.StatusCode == 201 {
		var result map[string]interface{}
		json.NewDecoder(resp2.Body).Decode(&result)
		if id, ok := result["projectID"].(string); ok {
			return id
		}
		// Fallback to "id" field for compatibility
		if id, ok := result["id"].(string); ok {
			return id
		}
	}

	return ""
}

func sendChatRequest(serverURL, projectID, message string) string {
	client := &http.Client{Timeout: 30 * time.Second}

	requestBody := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": message},
		},
		"stream":    false, // Use non-streaming for simplicity in interactive mode
		"projectID": projectID,
		"retrieval": map[string]int{"k": 5},
	}

	jsonData, _ := json.Marshal(requestBody)
	resp, err := client.Post(serverURL+"/chat", "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Sprintf("❌ Error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("❌ Server error: %s - %s", resp.Status, string(body))
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Sprintf("❌ Failed to parse response: %v", err)
	}

	if content, ok := response["content"].(string); ok {
		return content
	}

	return "❌ No response content"
}

func printInteractiveHelp() {
	fmt.Println("🔧 Interactive Chat Commands:")
	fmt.Println("  /help, /h          - Show this help")
	fmt.Println("  /exit, /quit, /q   - Exit interactive mode")
	fmt.Println("  /clear             - Clear screen")
	fmt.Println("  /project list      - List projects")
	fmt.Println("  /project <name>    - Switch to project")
	fmt.Println("  /index             - Index current project")
	fmt.Println("  <your question>    - Ask anything about the code")
	fmt.Println()
	fmt.Println("💡 Examples:")
	fmt.Println("  > What is this project about?")
	fmt.Println("  > How does the server.go file work?")
	fmt.Println("  > Show me the REST API endpoints")
	fmt.Println("  > Help me add a new feature")
}

func clearScreen() {
	// Simple clear screen for Unix-like systems
	fmt.Print("\033[2J\033[H")
}

func handleProjectCommand(input, serverURL string) {
	parts := strings.Split(input, " ")
	if len(parts) < 2 {
		fmt.Println("Usage: /project list|<name>")
		return
	}

	if parts[1] == "list" {
		client := &http.Client{}
		resp, err := client.Get(serverURL + "/projects")
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		defer resp.Body.Close()

		var projects []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&projects)

		fmt.Println("📁 Available projects:")
		for _, p := range projects {
			fmt.Printf("  - %s (ID: %s)\n", p["name"], p["id"])
		}
	} else {
		fmt.Printf("🔄 Project switching to '%s' not implemented yet\n", parts[1])
	}
}

func handleIndexCommand(input, projectID, serverURL string) {
	fmt.Printf("🔄 Indexing project %s...\n", projectID)

	client := &http.Client{}
	requestBody := map[string]interface{}{
		"projectID": projectID,
		"mode":      "full",
	}

	jsonData, _ := json.Marshal(requestBody)
	resp, err := client.Post(serverURL+"/index/run", "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Println("✅ Indexing started successfully")
	} else {
		fmt.Printf("❌ Indexing failed: %s\n", resp.Status)
	}
}

// shouldIndexProject checks if the project needs indexing
func shouldIndexProject(serverURL, projectID string) bool {
	// Check if project has been indexed before
	client := &http.Client{Timeout: 2 * time.Second}
	
	// Try to search for a test query to see if index exists
	testQuery := "main"
	url := fmt.Sprintf("%s/search?q=%s&projectID=%s", serverURL, testQuery, projectID)
	
	resp, err := client.Get(url)
	if err != nil {
		// If error, assume needs indexing
		return true
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		// If not successful, needs indexing
		return true
	}
	
	// Check if we have any results
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return true
	}
	
	// If results exist, check if they're empty
	if results, ok := result["results"].([]interface{}); ok {
		if len(results) == 0 {
			// No indexed documents, needs indexing
			return true
		}
		// Has indexed documents, check age (for now, skip if already indexed)
		return false
	}
	
	// Default to indexing if uncertain
	return true
}

// indexProjectInBackground indexes the project in the background
func indexProjectInBackground(serverURL, projectID string) {
	client := &http.Client{Timeout: 60 * time.Second}
	requestBody := map[string]interface{}{
		"projectID": projectID,
		"mode":      "full",
		"maxFiles":  1000,  // Limit for initial indexing
	}

	jsonData, _ := json.Marshal(requestBody)
	resp, err := client.Post(serverURL+"/index/run", "application/json", strings.NewReader(string(jsonData)))
	if err != nil {
		fmt.Printf("\n⚠️  Background indexing error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if jobID, ok := result["jobID"].(string); ok {
				// Monitor job status
				monitorIndexingJob(serverURL, jobID)
			}
		}
	}
}

// monitorIndexingJob monitors the indexing job status
func monitorIndexingJob(serverURL, jobID string) {
	client := &http.Client{Timeout: 5 * time.Second}
	maxAttempts := 30 // Monitor for max 30 seconds
	
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(1 * time.Second)
		
		resp, err := client.Get(fmt.Sprintf("%s/index/jobs/%s", serverURL, jobID))
		if err != nil {
			continue
		}
		
		var job map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		
		if status, ok := job["status"].(string); ok {
			if status == "completed" {
				// Silently complete - don't interrupt user's input
				return
			} else if status == "failed" {
				// Silently fail - don't interrupt user's input
				return
			}
		}
	}
}
