package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: punch <serve|add|next|update|attach|list|get|rm|memory|config> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "add":
		cmdAdd(os.Args[2:])
	case "next":
		cmdNext(os.Args[2:])
	case "update":
		cmdUpdate(os.Args[2:])
	case "attach":
		cmdAttach(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "get":
		cmdGet(os.Args[2:])
	case "rm":
		cmdRm(os.Args[2:])
	case "memory":
		cmdMemory(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "bind address")
	token := fs.String("token", "", "bearer token (auth off if empty)")
	store := fs.String("store", "tasks.json", "path to tasks.json")
	memory := fs.String("memory", "memory.json", "path to memory.json")
	originBase := fs.String("public-url", "", "public origin for absolute artifact URLs (optional)")
	trustedProxy := fs.Bool("trusted-proxy", false, "honor X-Forwarded-* (only behind a trusted proxy)")
	insecure := fs.Bool("insecure", false, "allow non-loopback bind without a token")
	maxUp := fs.Int64("max-upload", maxUploadDefault, "max upload bytes")
	fs.Parse(args)

	if err := validateBind(*addr, *token, *insecure); err != nil {
		fail("%v", err)
	}
	maxUpload = *maxUp

	s, err := NewStore(*store)
	if err != nil {
		fail("store: %v", err)
	}
	sweepStuck(s) // Task 9: flag long-running in_progress on startup

	ms, err := NewMemoryStore(*memory)
	if err != nil {
		fail("memory store: %v", err)
	}

	var h http.Handler = newMux(s, ms, *originBase)
	h = proxyMiddleware(*trustedProxy)(h)
	h = tokenMiddleware(*token)(h)
	log.Printf("punchcard serving on %s (auth=%v)", *addr, *token != "")
	log.Fatal(http.ListenAndServe(*addr, h))
}

func cmdAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	title := fs.String("title", "", "task title (required)")
	desc := fs.String("description", "", "description / brief")
	acc := fs.String("acceptance", "", "acceptance criteria")
	repo := fs.String("repo", "", "local repo path")
	prio := fs.Int("priority", 1, "priority (higher first)")
	fs.Parse(args)
	if *title == "" {
		fail("--title required")
	}
	code, body, err := doJSON("POST", "/api/tasks", map[string]any{
		"title": *title, "description": *desc, "acceptance": *acc, "repo": *repo, "priority": *prio,
	})
	if err != nil || code != http.StatusCreated {
		fail("add failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

func cmdNext(args []string) {
	code, body, err := doJSON("POST", "/api/next", nil)
	if err != nil {
		fail("next: %v", err)
	}
	if code == http.StatusNoContent {
		os.Exit(3) // distinct exit code => loop knows the queue is drained
	}
	fmt.Println(string(body))
}

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	status := fs.String("status", "", "status")
	pr := fs.String("pr", "", "pr url")
	branch := fs.String("branch", "", "branch")
	note := fs.String("note", "", "note")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		fail("usage: punch update <id> [--status ...] [--pr ...] [--branch ...] [--note ...]")
	}
	payload := map[string]any{}
	if *status != "" {
		payload["status"] = *status
	}
	if *pr != "" {
		payload["pr_url"] = *pr
	}
	if *branch != "" {
		payload["branch"] = *branch
	}
	if *note != "" {
		payload["note"] = *note
	}
	code, body, err := doJSON("PATCH", "/api/tasks/"+rest[0], payload)
	if err != nil || code != http.StatusOK {
		fail("update failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

func cmdAttach(args []string) {
	if len(args) < 2 {
		fail("usage: punch attach <id> <file>")
	}
	code, body, err := uploadFile(args[0], args[1])
	if err != nil || code != http.StatusCreated {
		fail("attach failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

func cmdList(args []string) {
	code, body, err := doJSON("GET", "/api/tasks", nil)
	if err != nil || code != http.StatusOK {
		fail("list failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

func cmdGet(args []string) {
	if len(args) < 1 {
		fail("usage: punch get <id>")
	}
	code, body, err := doJSON("GET", "/api/tasks/"+args[0], nil)
	if err != nil || code != http.StatusOK {
		fail("get failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

func cmdRm(args []string) {
	if len(args) < 1 {
		fail("usage: punch rm <id>")
	}
	code, body, err := doDelete("/api/tasks/" + args[0])
	if err != nil || code != http.StatusNoContent {
		fail("rm failed (%d): %s %v", code, body, err)
	}
	fmt.Println("deleted", args[0])
}

// cmdMemory dispatches memory subcommands: add, search, list, get, rm.
func cmdMemory(args []string) {
	if len(args) < 1 {
		fail("usage: punch memory <add|search|list|get|rm> [flags]")
	}
	switch args[0] {
	case "add":
		cmdMemoryAdd(args[1:])
	case "search":
		cmdMemorySearch(args[1:])
	case "list":
		cmdMemoryList(args[1:])
	case "get":
		cmdMemoryGet(args[1:])
	case "rm":
		cmdMemoryRm(args[1:])
	default:
		fail("unknown memory subcommand %q", args[0])
	}
}

func cmdMemoryAdd(args []string) {
	fs := flag.NewFlagSet("memory add", flag.ExitOnError)
	title := fs.String("title", "", "note title (required)")
	body := fs.String("body", "", "note body (reads stdin if empty)")
	repo := fs.String("repo", "", "repo path (optional)")
	tags := fs.String("tags", "", "comma-separated tags (optional)")
	fs.Parse(args)

	if *title == "" {
		fail("--title required")
	}
	noteBody := *body
	if noteBody == "" {
		data, err := readStdin()
		if err != nil {
			fail("reading stdin: %v", err)
		}
		noteBody = strings.TrimRight(string(data), "\n")
	}
	var tagList []string
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tagList = append(tagList, t)
			}
		}
	}
	payload := map[string]any{
		"title": *title,
		"body":  noteBody,
		"repo":  *repo,
		"tags":  tagList,
	}
	code, respBody, err := doJSON("POST", "/api/memory", payload)
	if err != nil || code != http.StatusCreated {
		fail("memory add failed (%d): %s %v", code, respBody, err)
	}
	fmt.Println(string(respBody))
}

func cmdMemorySearch(args []string) {
	fs := flag.NewFlagSet("memory search", flag.ExitOnError)
	repo := fs.String("repo", "", "filter by repo (optional)")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		fail("usage: punch memory search <query> [--repo ...]")
	}
	q := rest[0]
	path := "/api/memory?q=" + urlEncode(q)
	if *repo != "" {
		path += "&repo=" + urlEncode(*repo)
	}
	code, body, err := doJSON("GET", path, nil)
	if err != nil || code != http.StatusOK {
		fail("memory search failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

func cmdMemoryList(args []string) {
	fs := flag.NewFlagSet("memory list", flag.ExitOnError)
	repo := fs.String("repo", "", "filter by repo (optional)")
	fs.Parse(args)
	path := "/api/memory"
	if *repo != "" {
		path += "?repo=" + urlEncode(*repo)
	}
	code, body, err := doJSON("GET", path, nil)
	if err != nil || code != http.StatusOK {
		fail("memory list failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

func cmdMemoryGet(args []string) {
	if len(args) < 1 {
		fail("usage: punch memory get <id>")
	}
	code, body, err := doJSON("GET", "/api/memory/"+args[0], nil)
	if err != nil || code != http.StatusOK {
		fail("memory get failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

func cmdMemoryRm(args []string) {
	if len(args) < 1 {
		fail("usage: punch memory rm <id>")
	}
	code, body, err := doDelete("/api/memory/" + args[0])
	if err != nil || code != http.StatusNoContent {
		fail("memory rm failed (%d): %s %v", code, body, err)
	}
}

func sweepStuck(s *Store) {
	if n := s.SweepStuck(30 * time.Minute); n > 0 {
		log.Printf("startup sweep: flagged %d stuck in_progress task(s) as failed", n)
	}
}

// cmdConfig dispatches config subcommands: set, show.
func cmdConfig(args []string) {
	if len(args) < 1 {
		fail("usage: punch config <set|show> [flags]")
	}
	switch args[0] {
	case "set":
		cmdConfigSet(args[1:])
	case "show":
		cmdConfigShow()
	default:
		fail("unknown config subcommand %q", args[0])
	}
}

// maskToken returns a masked representation of a token for display.
// Shows "set (last 4: XXXX)" if the token is long enough, or "set" otherwise.
func maskToken(tok string) string {
	if len(tok) >= 4 {
		return "set (last 4: " + tok[len(tok)-4:] + ")"
	}
	return "set"
}

func cmdConfigSet(args []string) {
	const sentinel = "\x00unset\x00"
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	urlFlag := fs.String("url", sentinel, "server URL")
	tokenFlag := fs.String("token", sentinel, "bearer token")
	fs.Parse(args)

	// Determine which flags were explicitly provided.
	urlSet := false
	tokenSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "url":
			urlSet = true
		case "token":
			tokenSet = true
		}
	})

	cfg := loadConfig()
	if urlSet {
		cfg.URL = *urlFlag
	}
	if tokenSet {
		cfg.Token = *tokenFlag
	}
	if err := saveConfig(cfg); err != nil {
		fail("config set: %v", err)
	}

	fmt.Printf("Config saved to %s\n", configPath())
	if urlSet {
		fmt.Printf("  url   = %s\n", cfg.URL)
	}
	if tokenSet {
		if cfg.Token != "" {
			fmt.Printf("  token = %s\n", maskToken(cfg.Token))
		} else {
			fmt.Printf("  token = (cleared)\n")
		}
	}
}

func cmdConfigShow() {
	path := configPath()
	cfg := loadConfig()

	url := resolvedURL()
	urlSource := "default"
	if os.Getenv("PUNCH_URL") != "" {
		urlSource = "env (PUNCH_URL)"
	} else if cfg.URL != "" {
		urlSource = "config file"
	}

	token := resolvedToken()
	tokenDesc := "not set"
	tokenSource := ""
	if token != "" {
		tokenDesc = maskToken(token)
		if os.Getenv("PUNCH_TOKEN") != "" {
			tokenSource = " [env: PUNCH_TOKEN]"
		} else {
			tokenSource = " [config file]"
		}
	}

	fmt.Printf("Config file : %s\n", path)
	fmt.Printf("URL         : %s [%s]\n", url, urlSource)
	fmt.Printf("Token       : %s%s\n", tokenDesc, tokenSource)
}
