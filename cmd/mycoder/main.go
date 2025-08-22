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
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"mycoder/internal/config"
	mylog "mycoder/internal/log"
	"mycoder/internal/server"
	"mycoder/internal/version"
)

func main() {
	// load config file and apply env (env has precedence)
	_ = config.LoadAndApply()
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
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
	fmt.Println("  mycoder exec -- -- <cmd> [args...]")
	fmt.Println("  mycoder explain --project <id> <path|symbol>")
	fmt.Println("  mycoder edit --project <id> --goal \"<설명>\" [--files a.go,b.go] [--stream]")
	fmt.Println("  mycoder mcp tools|call --name <tool> --json '<params>'")
	fmt.Println("  mycoder test --project <id> [--timeout 60] [--verbose]")
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
		ctx, cancel := signalContext()
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL()+"/index/run/stream", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
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
				switch lastEvent {
				case "job":
					jobID = data
					fmt.Printf("job: %s\n", jobID)
				case "progress":
					// parse {indexed,total}
					var p struct {
						Indexed int `json:"indexed"`
						Total   int `json:"total"`
					}
					_ = json.Unmarshal([]byte(data), &p)
					total = p.Total
					indexed = p.Indexed
					// simple progress line
					fmt.Printf("progress: %d/%d\n", indexed, total)
				case "completed":
					fmt.Println("completed")
				case "error":
					fmt.Fprintln(os.Stderr, data)
				default:
					// ignore
				}
			}
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

func modelsCmd(args []string) {
	fs := flag.NewFlagSet("models", flag.ExitOnError)
	format := fs.String("format", "table", "output format: table|json|raw")
	filter := fs.String("filter", "", "substring filter for model id")
	color := fs.Bool("color", false, "enable ANSI colors for table")
	_ = fs.Parse(args)
	base := os.Getenv("MYCODER_OPENAI_BASE_URL")
	if base == "" {
		base = "http://192.168.0.227:3620/v1"
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

func colorCyan(s string) string { return "\x1b[36m" + s + "\x1b[0m" }

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
		ctx2, cancel2 := signalContext()
		defer cancel2()
		req, _ := http.NewRequestWithContext(ctx2, http.MethodPost, serverURL()+"/shell/exec/stream", strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		rd := bufio.NewScanner(resp.Body)
		lastEvent := ""
		exitCode := 0
		limited := false
		// optional tail buffers
		var bufOut, bufErr []string
		push := func(buf *[]string, line string, max int) {
			if max <= 0 {
				return
			}
			*buf = append(*buf, line)
			if len(*buf) > max {
				*buf = (*buf)[len(*buf)-max:]
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
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
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
			fmt.Printf("  %s %s%s\n", mark, k, suffix)
			if v.Suggestion != "" {
				fmt.Printf("    Hint: %s\n", v.Suggestion)
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
		fmt.Printf("  %s %s%s\n", mark, k, suffix)
		if v.Suggestion != "" {
			fmt.Printf("    Hint: %s\n", v.Suggestion)
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
	fmt.Println(res.Content)
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
