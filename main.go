package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/core"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/mcp"
	"github.com/lucasvidela94/parley/internal/skills"
	"github.com/lucasvidela94/parley/internal/store"
	"github.com/lucasvidela94/parley/internal/update"
)

// version is set at build time with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "parley: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println("parley " + version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "mcp":
		return cmdMCP()
	case "participants":
		return cmdParticipants()
	case "send":
		return cmdSend(args[1:])
	case "open":
		return cmdOpen(args[1:])
	case "read":
		return cmdRead(args[1:])
	case "close":
		return cmdClose(args[1:])
	case "list":
		return cmdList(args[1:])
	case "session":
		return cmdSession(args[1:])
	case "update":
		return cmdUpdate(args[1:])
	case "setup":
		return cmdSetup(args[1:])
	case "harnesses", "profiles":
		return cmdHarnesses()
	case "probe":
		return cmdProbe(args[1:])
	default:
		return fmt.Errorf("unknown command: %s (try `parley help`)", args[0])
	}
}

func usage() {
	fmt.Print(`parley — a channel between agent harnesses

  parley send --to <name> [--in <id>] [--context <text>] [--write] [message]
  parley open --topic <topic> [--context <text>]
  parley read <id>
  parley close <id> [--note <text>]
  parley list [--limit 20]
  parley participants
  parley session reset <name>
  parley update [--check] [--force]
  parley mcp                 # MCP server over stdio (launched by the harness)
  parley setup [--write]     # register the MCP server in every detected harness
  parley harnesses           # known CLIs and which ones are verified
  parley probe -- <cmd...>   # inspect a CLI to build its spec

send without --in creates an ephemeral one-turn conversation.
Local identity: PARLEY_IDENTITY. Config: parley setup.
`)
}

func openEngine() (*core.Engine, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.StorePath())
	if err != nil {
		return nil, err
	}
	return core.New(cfg, st), nil
}

func cmdMCP() error {
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	return mcp.New(e, version).Serve(context.Background(), os.Stdin, os.Stdout, os.Stderr)
}

func cmdParticipants() error {
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	for _, p := range e.Participants() {
		fmt.Printf("%-10s %-12s %-34s %s\n", p["name"], p["harness"], defStr(p["model"], "(harness default model)"), p["permission"])
	}
	fmt.Printf("\nlocal identity: %s\nstore: %s\n", e.Identity(), e.Config().StorePath())
	return nil
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "target participant")
	in := fs.String("in", "", "conversation id to continue")
	contextStr := fs.String("context", "", "extra context")
	write := fs.Bool("write", false, "allow file edits")
	timeout := fs.Int("timeout", 0, "timeout in seconds")
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(args)

	msg := strings.Join(fs.Args(), " ")
	if msg == "" {
		raw, _ := io.ReadAll(os.Stdin)
		msg = string(raw)
	}
	if *to == "" || strings.TrimSpace(msg) == "" {
		return errors.New("missing --to and message")
	}
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	res, err := e.Send(context.Background(), core.SendRequest{
		ConversationID: *in, To: *to, Message: msg, Context: *contextStr, Write: *write, TimeoutS: *timeout,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(res)
	}
	fmt.Println(strings.TrimSpace(res.Reply.Body))
	if res.Conversation.Status == "closed" {
		fmt.Fprintf(os.Stderr, "\n[ephemeral conversation %s]\n", res.Conversation.ID)
	}
	return nil
}

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	topic := fs.String("topic", "", "topic")
	contextStr := fs.String("context", "", "shared context")
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(args)
	if *topic == "" {
		*topic = strings.Join(fs.Args(), " ")
	}
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	view, err := e.Open(*topic, *contextStr)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(view)
	}
	fmt.Println(view.Markdown())
	return nil
}

func cmdRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: parley read <id>")
	}
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	view, err := e.Read(fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(view)
	}
	fmt.Println(view.Markdown())
	return nil
}

func cmdClose(args []string) error {
	fs := flag.NewFlagSet("close", flag.ExitOnError)
	note := fs.String("note", "", "closing note")
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: parley close <id> [--note <text>]")
	}
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	view, err := e.Close(fs.Arg(0), *note)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(view)
	}
	fmt.Println(view.Markdown())
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int("limit", 20, "how many")
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(args)
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	dir := ""
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	list, err := e.List(dir, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(list)
	}
	for _, c := range list {
		fmt.Printf("%s  %-7s %s\n", c.ID, c.Status, c.Topic)
	}
	return nil
}

func cmdSession(args []string) error {
	if len(args) < 2 || args[0] != "reset" {
		return errors.New("usage: parley session reset <participant>")
	}
	e, err := openEngine()
	if err != nil {
		return err
	}
	defer e.Shutdown()
	if err := e.ResetSession(args[1], ""); err != nil {
		return err
	}
	fmt.Printf("session for %s cleared; the next turn starts with no prior context\n", args[1])
	return nil
}

func cmdHarnesses() error {
	for _, name := range harness.Names() {
		s, _ := harness.Known(name)
		status := "unverified"
		if s.Verified {
			status = "verified"
		}
		fmt.Printf("%-9s %-13s %-14s %s\n", name, s.Bin, status, s.Notes)
	}
	fmt.Println("\nto add one: copy a config block and adjust bin/start/continue/text_path")
	return nil
}

type integration struct {
	name   string
	bin    string
	manual string
	apply  func(exe string) error
}

// integrations is the only place that knows a harness's MCP registration
// syntax. Everything else treats harnesses uniformly.
func integrations(exe string) []integration {
	return []integration{
		{
			name: "claude", bin: "claude",
			manual: fmt.Sprintf("claude mcp add --scope user parley -e PARLEY_IDENTITY=claude -- %s mcp", exe),
			apply: func(exe string) error {
				_ = exec.Command("claude", "mcp", "remove", "--scope", "user", "parley").Run()
				out, err := exec.Command("claude", "mcp", "add", "--scope", "user", "parley", "-e", "PARLEY_IDENTITY=claude", "--", exe, "mcp").CombinedOutput()
				if err != nil {
					return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
				}
				return nil
			},
		},
		{
			name: "opencode", bin: "opencode",
			manual: fmt.Sprintf("mcp.servers.parley = {type:local, command:[%q,\"mcp\"], environment:{PARLEY_IDENTITY:\"opencode\"}} in %s", exe, opencodeConfigPath()),
			apply:  func(exe string) error { return patchOpencode(opencodeConfigPath(), exe) },
		},
		{
			name: "codex", bin: "codex",
			manual: fmt.Sprintf("codex mcp add parley -- %s mcp", exe),
		},
		{
			name: "cursor", bin: "cursor-agent",
			manual: fmt.Sprintf("configure the MCP server by hand: command [%q,\"mcp\"]", exe),
		},
		{
			name: "pi", bin: "pi",
			manual: fmt.Sprintf("configure the MCP server by hand: command [%q,\"mcp\"]", exe),
		},
	}
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report available versions")
	force := fs.Bool("force", false, "replace a development build too")
	_ = fs.Parse(args)

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := update.New("lucasvidela94", "parley")
	rel, err := client.Latest(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("current %s · latest %s\n", version, rel.Tag)

	if version == "dev" && !*force {
		fmt.Println("development build; run with --force to replace it")
		return nil
	}
	if !update.IsNewer(version, rel.Tag) {
		fmt.Println("already up to date")
		return nil
	}
	if *check {
		fmt.Printf("update available: %s (%s)\n", rel.Tag, rel.Asset)
		return nil
	}
	if err := client.Install(ctx, rel, exe); err != nil {
		return err
	}
	fmt.Printf("updated to %s (%s)\n", rel.Tag, exe)
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	write := fs.Bool("write", false, "apply the changes")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	detected := config.Detect()

	fmt.Printf("config: %s\nstore:  %s\nbinary: %s\n", cfg.Path(), cfg.StorePath(), exe)
	if len(cfg.Participants) == 0 {
		fmt.Printf("no participants; harnesses found on PATH: %s\n", joinNames(detected))
	}
	if *write {
		var added []string
		for name, p := range detected {
			if _, ok := cfg.Participants[name]; !ok {
				cfg.Participants[name] = p
				added = append(added, name)
			}
		}
		if len(added) > 0 {
			sort.Strings(added)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("participants added: %s\n", strings.Join(added, ", "))
		}
	}
	fmt.Println()

	for _, p := range sortedNames(cfg.Participants) {
		bin := ""
		if s, _, err := cfg.Spec(p); err == nil {
			bin = s.Bin
		}
		if _, err := exec.LookPath(bin); err == nil {
			fmt.Printf("  ok      %s (%s)\n", p, bin)
		} else {
			fmt.Printf("  missing %s (%s is not on PATH)\n", p, bin)
		}
	}

	fmt.Println()
	for _, in := range integrations(exe) {
		if _, err := exec.LookPath(in.bin); err != nil {
			continue
		}
		fmt.Printf("%s: %s\n", in.name, in.manual)
		if !*write || in.apply == nil {
			continue
		}
		if err := in.apply(exe); err != nil {
			fmt.Printf("  failed: %v\n", err)
		} else {
			fmt.Println("  configured")
		}
	}

	if *write {
		for name, err := range skills.Install() {
			if err != nil {
				fmt.Printf("skill %s: %v\n", name, err)
			} else {
				fmt.Printf("parley skill installed for %s\n", name)
			}
		}
	} else {
		fmt.Println("parley skill: copied into the skill directory of every detected harness")
		fmt.Println("\n(nothing written; run `parley setup --write` to apply)")
	}
	return nil
}

func joinNames(m map[string]config.Participant) string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

func cmdProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	prompt := fs.String("prompt", "responde solo: ok", "test prompt")
	viaStdin := fs.Bool("stdin", false, "send the prompt over stdin")
	timeout := fs.Int("timeout", 120, "timeout in seconds")
	_ = fs.Parse(args)
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("usage: parley probe [--stdin] -- <command...>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	if *viaStdin {
		cmd.Stdin = strings.NewReader(*prompt)
	} else {
		cmd.Args = append(cmd.Args, *prompt)
	}
	out, err := cmd.Output()
	code := 0
	if err != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintf(os.Stderr, "exit %d\nstderr: %s\n", ee.ExitCode(), ee.Stderr)
		}
	}
	fmt.Printf("exit=%d bytes=%d\n", code, len(out))
	if err != nil && len(out) == 0 {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	shapes := map[string]bool{}
	for _, line := range lines {
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) == nil {
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			shapes["{"+strings.Join(keys, ",")+"}"] = true
		}
	}
	if len(shapes) > 0 {
		fmt.Println("shapes:")
		for s := range shapes {
			fmt.Println("  " + s)
		}
	}
	if len(lines) > 6 {
		lines = append(lines[:3], "…")
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// --- setup helpers ---------------------------------------------------------

func opencodeConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "opencode", "opencode.json")
}

func patchOpencode(path, exe string) error {
	raw, err := os.ReadFile(path)
	existed := err == nil
	if os.IsNotExist(err) {
		raw = []byte("{}\n")
		err = nil
	}
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("the config has a format I cannot touch without breaking it: %w", err)
	}
	if existed {
		if err := os.WriteFile(path+".bak", raw, 0o644); err != nil {
			return err
		}
	}
	mcpSection, _ := cfg["mcp"].(map[string]any)
	if mcpSection == nil {
		mcpSection = map[string]any{}
		cfg["mcp"] = mcpSection
	}
	servers, _ := mcpSection["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		mcpSection["servers"] = servers
	}
	servers["parley"] = map[string]any{
		"type":        "local",
		"command":     []any{exe, "mcp"},
		"environment": map[string]any{"PARLEY_IDENTITY": "opencode"},
		"timeout":     600000,
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func sortedNames(m map[string]config.Participant) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func jsonOut(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func defStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
