package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: punch <serve|add|next|update|attach|list|get> [flags]")
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
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "bind address")
	token := fs.String("token", "", "bearer token (auth off if empty)")
	store := fs.String("store", "tasks.json", "path to tasks.json")
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

	var h http.Handler = newMux(s, *originBase)
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

func sweepStuck(s *Store) {
	if n := s.SweepStuck(30 * time.Minute); n > 0 {
		log.Printf("startup sweep: flagged %d stuck in_progress task(s) as failed", n)
	}
}
