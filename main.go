package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: punch <serve|add|next|update|attach|cancel|list|get|rm|pause|resume|stop|concurrency|export|import|memory|config> [flags]")
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
	case "cancel":
		cmdCancel(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "pending-merges":
		cmdPendingMerges()
	case "export":
		cmdExport(os.Args[2:])
	case "import":
		cmdImport(os.Args[2:])
	case "get":
		cmdGet(os.Args[2:])
	case "rm":
		cmdRm(os.Args[2:])
	case "pause":
		cmdPause(true)
	case "resume":
		cmdPause(false)
	case "stop":
		cmdStop()
	case "concurrency":
		cmdConcurrency(os.Args[2:])
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
	control := fs.String("control", "control.json", "path to control.json")
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

	cstore, err := NewControlStore(*control)
	if err != nil {
		fail("control store: %v", err)
	}

	var h http.Handler = newMux(s, ms, cstore, *originBase)
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
	deps := fs.String("depends-on", "", "comma-separated task ids that must be merged before this is claimed")
	force := fs.Bool("force", false, "add even if a similar active task already exists")
	fs.Parse(args)
	if *title == "" {
		fail("--title required")
	}
	var depList []string
	for _, d := range strings.Split(*deps, ",") {
		if d = strings.TrimSpace(d); d != "" {
			depList = append(depList, d)
		}
	}
	code, body, err := doJSON("POST", "/api/tasks", map[string]any{
		"title": *title, "description": *desc, "acceptance": *acc, "repo": *repo,
		"priority": *prio, "depends_on": depList, "force": *force,
	})
	if code == http.StatusConflict {
		fail("a similar active task already exists (re-run with --force to add anyway):\n%s", body)
	}
	if err != nil || code != http.StatusCreated {
		fail("add failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

func cmdNext(args []string) {
	fs := flag.NewFlagSet("next", flag.ExitOnError)
	batch := fs.Bool("batch", false, "claim up to the server's concurrency limit (returns a JSON array)")
	fs.Parse(args)
	path := "/api/next"
	if *batch {
		path += "?batch=1"
	}
	code, body, err := doJSON("POST", path, nil)
	if err != nil {
		fail("next: %v", err)
	}
	switch code {
	case http.StatusNoContent:
		os.Exit(3) // queue drained => loop stops
	case http.StatusLocked:
		os.Exit(4) // paused => loop should idle and re-check, NOT stop
	}
	fmt.Println(string(body))
}

// cmdCancel requests cancellation: a running task's subagent aborts at its next
// checkpoint. `punch cancel <id>` cancels one; `punch cancel --all` is the
// kill-switch that cancels every in_progress task at once.
func cmdCancel(args []string) {
	if len(args) >= 1 && args[0] == "--all" {
		code, body, err := doJSON("POST", "/api/cancel-all", nil)
		if err != nil || code != http.StatusOK {
			fail("cancel --all failed (%d): %s %v", code, body, err)
		}
		fmt.Println(string(body))
		return
	}
	if len(args) < 1 {
		fail("usage: punch cancel <id> | punch cancel --all")
	}
	code, body, err := doJSON("PATCH", "/api/tasks/"+args[0], map[string]any{"status": string(StatusCancelled)})
	if err != nil || code != http.StatusOK {
		fail("cancel failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

func cmdPause(paused bool) {
	patch := map[string]any{"paused": paused}
	if !paused {
		patch["stopped"] = false // resume clears a hard stop too
	}
	code, body, err := doJSON("PATCH", "/api/control", patch)
	if err != nil || code != http.StatusOK {
		fail("control failed (%d): %s %v", code, body, err)
	}
	fmt.Println(string(body))
}

// cmdStop is the hard kill-switch: pause + set the stopped flag (the PreToolUse
// hook halts a running loop/subagent on its next tool call) + cancel everything
// in progress. Clear it with `punch resume`.
func cmdStop() {
	code, body, err := doJSON("PATCH", "/api/control", map[string]any{"paused": true, "stopped": true})
	if err != nil || code != http.StatusOK {
		fail("stop failed (%d): %s %v", code, body, err)
	}
	if c, b, e := doJSON("POST", "/api/cancel-all", nil); e != nil || c != http.StatusOK {
		fail("stop cancel-all failed (%d): %s %v", c, b, e)
	}
	fmt.Println(string(body))
}

// cmdConcurrency with no arg prints current control state; with N sets the
// concurrency (engineer subagents allowed to run at once).
func cmdConcurrency(args []string) {
	if len(args) == 0 {
		code, body, err := doJSON("GET", "/api/control", nil)
		if err != nil || code != http.StatusOK {
			fail("control failed (%d): %v", code, err)
		}
		fmt.Println(string(body))
		return
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		fail("usage: punch concurrency [N>=1]")
	}
	code, body, e := doJSON("PATCH", "/api/control", map[string]any{"concurrency": n})
	if e != nil || code != http.StatusOK {
		fail("control failed (%d): %s %v", code, body, e)
	}
	fmt.Println(string(body))
}

func cmdUpdate(args []string) {
	if len(args) < 1 {
		fail("usage: punch update <id> [--status ...] [--pr ...] [--branch ...] [--note ...] [--merged]")
	}
	id := args[0] // id is positional-first; flags follow (Go's flag pkg stops at the first non-flag)
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	status := fs.String("status", "", "status")
	pr := fs.String("pr", "", "pr url")
	branch := fs.String("branch", "", "branch")
	note := fs.String("note", "", "note")
	merged := fs.Bool("merged", false, "mark this task's PR as merged (unblocks dependents)")
	fs.Parse(args[1:])
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
	if *merged {
		payload["merged"] = true
	}
	code, body, err := doJSON("PATCH", "/api/tasks/"+id, payload)
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

// cmdPendingMerges prints the done-but-unmerged tasks that are blocking a todo
// (via depends_on) — the minimal set the loop checks for merge each tick.
func cmdPendingMerges() {
	code, body, err := doJSON("GET", "/api/pending-merges", nil)
	if err != nil || code != http.StatusOK {
		fail("pending-merges failed (%d): %v", code, err)
	}
	fmt.Println(string(body))
}

// cmdExport writes the board (tasks + memory) as a JSON bundle to stdout or --out.
func cmdExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	out := fs.String("out", "", "write to this file instead of stdout")
	fs.Parse(args)
	code, body, err := doJSON("GET", "/api/export", nil)
	if err != nil || code != http.StatusOK {
		fail("export failed (%d): %v", code, err)
	}
	if *out == "" {
		fmt.Println(string(body))
		return
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fail("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "exported %s\n", *out)
}

// cmdImport loads a bundle into the board. It refuses a non-empty board unless
// --replace is given.
func cmdImport(args []string) {
	// Split the positional file from flags so order doesn't matter (Go's flag pkg
	// otherwise stops parsing at the first non-flag arg).
	var file string
	var flags []string
	for _, a := range args {
		if file == "" && !strings.HasPrefix(a, "-") {
			file = a
		} else {
			flags = append(flags, a)
		}
	}
	if file == "" {
		fail("usage: punch import <file> [--replace]")
	}
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	replace := fs.Bool("replace", false, "overwrite a non-empty board")
	fs.Parse(flags)
	data, err := os.ReadFile(file)
	if err != nil {
		fail("read %s: %v", file, err)
	}
	path := "/api/import"
	if *replace {
		path += "?replace=true"
	}
	code, body, err := doJSON("POST", path, json.RawMessage(data)) // RawMessage is sent as-is, not re-encoded
	if err != nil {
		fail("import: %v", err)
	}
	if code == http.StatusConflict {
		fail("target board is not empty — re-run with --replace to overwrite it")
	}
	if code != http.StatusOK {
		fail("import failed (%d): %s", code, body)
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
	if len(args) < 1 {
		fail("usage: punch memory search <query> [--repo ...]")
	}
	q := args[0] // query is positional-first; --repo follows
	fs := flag.NewFlagSet("memory search", flag.ExitOnError)
	repo := fs.String("repo", "", "filter by repo (optional)")
	fs.Parse(args[1:])
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

// cmdConfig dispatches config subcommands. set/use/list/show are user-facing;
// url/token print the resolved values (used by scripts and the kill-switch hook).
func cmdConfig(args []string) {
	if len(args) < 1 {
		fail("usage: punch config <set|use|list|show> [flags]")
	}
	switch args[0] {
	case "set":
		cmdConfigSet(args[1:])
	case "use":
		cmdConfigUse(args[1:])
	case "list":
		cmdConfigList()
	case "show":
		cmdConfigShow()
	case "url":
		fmt.Println(resolvedURL())
	case "token":
		fmt.Println(resolvedToken())
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
	profileFlag := fs.String("profile", "", "profile name (default: current profile, or 'default')")
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
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	migrateLegacy(&cfg)

	name := *profileFlag
	if name == "" {
		name = cfg.Current
	}
	if name == "" {
		name = "default"
	}

	p := cfg.Profiles[name]
	if urlSet {
		p.URL = *urlFlag
	}
	if tokenSet {
		p.Token = *tokenFlag
	}
	cfg.Profiles[name] = p
	if cfg.Current == "" {
		cfg.Current = name // first profile becomes the active default
	}
	if err := saveConfig(cfg); err != nil {
		fail("config set: %v", err)
	}

	fmt.Printf("Config saved to %s (profile %q)\n", configPath(), name)
	if urlSet {
		fmt.Printf("  url   = %s\n", p.URL)
	}
	if tokenSet {
		if p.Token != "" {
			fmt.Printf("  token = %s\n", maskToken(p.Token))
		} else {
			fmt.Printf("  token = (cleared)\n")
		}
	}
	if cfg.Current == name {
		fmt.Println("  (active profile)")
	} else {
		fmt.Printf("  (switch to it with: punch config use %s)\n", name)
	}
}

func cmdConfigUse(args []string) {
	if len(args) < 1 {
		fail("usage: punch config use <profile>")
	}
	name := args[0]
	cfg := loadConfig()
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	migrateLegacy(&cfg)
	if _, ok := cfg.Profiles[name]; !ok {
		fail("no profile %q — define it first: punch config set --profile %s --url ... --token ...", name, name)
	}
	cfg.Current = name
	if err := saveConfig(cfg); err != nil {
		fail("config use: %v", err)
	}
	fmt.Printf("Now using profile %q\n", name)
}

func cmdConfigList() {
	cfg := loadConfig()
	migrateLegacy(&cfg)
	if len(cfg.Profiles) == 0 {
		fmt.Println("No profiles. Add one: punch config set --profile <name> --url ... --token ...")
		return
	}
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		marker := "  "
		if n == cfg.Current {
			marker = "* "
		}
		p := cfg.Profiles[n]
		tok := "(no token)"
		if p.Token != "" {
			tok = maskToken(p.Token)
		}
		fmt.Printf("%s%-14s %-32s %s\n", marker, n, p.URL, tok)
	}
	if env := os.Getenv("PUNCH_PROFILE"); env != "" {
		fmt.Printf("\nPUNCH_PROFILE=%s overrides the active profile in this shell.\n", env)
	}
}

func cmdConfigShow() {
	path := configPath()
	cfg := loadConfig()
	active := activeProfileName(cfg)
	_, profileExists := cfg.Profiles[active]

	profSource := "default"
	if os.Getenv("PUNCH_PROFILE") != "" {
		profSource = "env (PUNCH_PROFILE)"
	} else if cfg.Current != "" {
		profSource = "config current"
	}

	url := resolvedURL()
	urlSource := "default"
	if os.Getenv("PUNCH_URL") != "" {
		urlSource = "env (PUNCH_URL)"
	} else if profileExists {
		urlSource = "profile " + active
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
		} else if profileExists {
			tokenSource = " [profile " + active + "]"
		} else {
			tokenSource = " [config file]"
		}
	}

	fmt.Printf("Config file : %s\n", path)
	fmt.Printf("Profile     : %s [%s]\n", active, profSource)
	fmt.Printf("URL         : %s [%s]\n", url, urlSource)
	fmt.Printf("Token       : %s%s\n", tokenDesc, tokenSource)
}
