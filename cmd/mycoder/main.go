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
	case "knowledge":
		knowledgeCmd(os.Args[2:])
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
