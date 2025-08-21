package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"mycoder/internal/server"
	"mycoder/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", ":8089", "listen address")
		_ = fs.Parse(os.Args[2:])
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
	case "hooks":
		hooksCmd(os.Args[2:])
	case "exec":
		execCmd(os.Args[2:])
	case "knowledge":
		knowledgeCmd(os.Args[2:])
	case "fs":
		fsCmd(os.Args[2:])
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
	fmt.Println("  mycoder <command> (coming soon): explain | edit | test | hooks | fs | exec | mcp")
}

func isKnownStub(cmd string) bool {
	known := []string{"ask", "chat", "explain", "edit", "test", "hooks", "fs", "exec", "mcp"}
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
	_ = fs.Parse(args)
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}
	body := fmt.Sprintf(`{"projectID":"%s","mode":"%s"}`, *project, *mode)
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
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Println("usage: mycoder chat [--project <id>] [--k 5] \"<prompt>\"")
		os.Exit(1)
	}
	q := strings.Join(rest, " ")
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":true,"projectID":"%s","retrieval":{"k":%d}}`, q, *project, *k)
	req, _ := http.NewRequest(http.MethodPost, serverURL()+"/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	rd := bufio.NewScanner(resp.Body)
	for rd.Scan() {
		line := rd.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}
			fmt.Print(data)
		}
	}
	fmt.Println()
}

func modelsCmd(args []string) {
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
	io.Copy(os.Stdout, resp.Body)
}

func metricsCmd(args []string) {
	resp, err := http.Get(serverURL() + "/metrics")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
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
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("[dry-run] write %s (len=%d)\n", *path, len(*content))
			return
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
		_ = fs.Parse(args[1:])
		if *project == "" || *path == "" {
			fmt.Println("--project and --path required")
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("[dry-run] patch %s start=%d length=%d replace_len=%d\n", *path, *start, *length, len(*replace))
			return
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
		req, _ := http.NewRequest(http.MethodPost, serverURL()+"/shell/exec/stream", strings.NewReader(string(b)))
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
		fmt.Println("usage: mycoder hooks run [--project <id>] [--targets fmt-check,test,lint] [--timeout 60] [--verbose]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("hooks run", flag.ExitOnError)
	project := fs.String("project", "", "project ID")
	targets := fs.String("targets", "", "comma-separated targets (fmt-check,test,lint)")
	timeout := fs.Int("timeout", 60, "timeout in seconds per target")
	verbose := fs.Bool("verbose", false, "print each target output")
	_ = fs.Parse(args[1:])
	if *project == "" {
		fmt.Println("--project required")
		os.Exit(1)
	}
	body := fmt.Sprintf(`{"projectID":"%s","targets":[%s],"timeoutSec":%d}`, *project, toJSONStringArray(*targets), *timeout)
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
			fmt.Printf("  %s %s\n", mark, k)
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
		fmt.Printf("  %s %s\n", mark, k)
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
