// Package registercmd is `herder register <kind> …`: one append to the agent
// store. The per-kind flag contract is agentstore.SpecFor; this package only
// parses flags into an Event and lets Append validate. It never talks to hcom or herdr and never blocks on anything but the
// store's bounded lock. Exit 0 append (or identical replay), 2 usage, 3 store
// unavailable.
package registercmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/herderstate"
)

// ExitUnavailable is the documented exit for "store unavailable".
const ExitUnavailable = 3

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage())
		return boolToCode(len(args) == 0)
	}
	kind := args[0]
	sp, ok := agentstore.SpecFor(kind)
	if !ok {
		fmt.Fprintf(stderr, "herder register: unknown kind %q\n%s", kind, usage())
		return 2
	}
	if len(args) > 1 && (args[1] == "-h" || args[1] == "--help") {
		fmt.Fprint(stdout, kindUsage(kind, sp))
		return 0
	}
	fs := flag.NewFlagSet("herder register "+kind, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	values := map[string]*string{}
	for _, name := range append(append(append(append([]string(nil), agentstore.CommonFlags...), sp.Required...), sp.OneOf...), sp.Optional...) {
		if _, dup := values[name]; dup {
			continue
		}
		values[name] = fs.String(name, "", "")
	}
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintf(stderr, "herder register %s: %v\n%s", kind, err, kindUsage(kind, sp))
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "herder register %s: unexpected argument %q\n", kind, fs.Arg(0))
		return 2
	}
	get := func(name string) string {
		if p := values[name]; p != nil {
			return strings.TrimSpace(*p)
		}
		return ""
	}
	now := time.Now().UTC()
	at := now
	if text := get("at"); text != "" {
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			fmt.Fprintf(stderr, "herder register %s: --at %q is not RFC3339\n", kind, text)
			return 2
		}
		at = parsed.UTC()
	}
	id := get("id")
	if id == "" {
		id = agentstore.NewID(now)
	} else if !agentstore.ValidID(id) {
		fmt.Fprintf(stderr, "herder register %s: --id %q is not UUID-shaped\n", kind, id)
		return 2
	}
	by := get("by")
	byKind := get("by-kind")
	if by == "" {
		by, byKind = defaultBy()
	}
	if byKind == "" {
		byKind = "agent"
	}
	e := agentstore.Event{
		ID: id, At: at, Kind: kind, By: by, ByKind: byKind, Name: get("name"), Request: get("request"),
		Tool: get("tool"), Model: get("model"), Effort: get("effort"), Tag: get("tag"), PromptRef: get("prompt-ref"),
		LauncherKind: get("launcher-kind"), Batch: get("batch"), Pane: get("pane"), Cwd: get("cwd"), Session: get("session"),
		Reason: get("reason"), Close: get("close"), FromSession: get("from-session"), FromName: get("from"),
		Mission: get("mission"), Brief: get("brief"), Thread: get("thread"), Task: get("task"), Title: get("title"), Note: get("note"),
		Manager: get("manager"), HcomEvent: get("hcom-event"), ParentName: get("parent-name"), Path: get("path"),
	}
	if ws, pane, split := get("workspace"), get("pane"), get("split-from"); kind == agentstore.KindLaunchRequested {
		e.Pane = ""
		e.Placement = &agentstore.Placement{Workspace: ws, Pane: pane, SplitFrom: split}
	} else if ws != "" {
		e.Placement = &agentstore.Placement{Workspace: ws}
	}
	if text := get("steer-chars"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil {
			fmt.Fprintf(stderr, "herder register %s: --steer-chars %q is not an integer\n", kind, text)
			return 2
		}
		e.SteerChars = &n
	}
	if text := get("instances"); text != "" {
		for _, part := range strings.Split(text, ",") {
			if part = strings.TrimSpace(part); part != "" {
				e.Instances = append(e.Instances, part)
			}
		}
	}
	if text := get("is-hcom-launched"); text != "" {
		v, err := strconv.ParseBool(text)
		if err != nil {
			fmt.Fprintf(stderr, "herder register %s: --is-hcom-launched %q is not a bool\n", kind, text)
			return 2
		}
		e.IsHcomLaunched = &v
	}
	if err := e.Validate(); err != nil {
		fmt.Fprintf(stderr, "herder register %s: %v\n", kind, err)
		return 2
	}

	stateDir, err := herderstate.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "herder register %s: store unavailable: %v\n", kind, err)
		return ExitUnavailable
	}
	store := agentstore.Open(stateDir, stderr)
	if store.ImportErr != nil {
		fmt.Fprintf(stderr, "herder register %s: store unavailable: %v\n", kind, store.ImportErr)
		return ExitUnavailable
	}
	receipt, err := store.Append(e)
	if err != nil {
		if errors.Is(err, agentstore.ErrUnavailable) {
			fmt.Fprintf(stderr, "herder register %s: %v\n", kind, err)
			return ExitUnavailable
		}
		fmt.Fprintf(stderr, "herder register %s: %v\n", kind, err)
		return 2
	}
	if *asJSON {
		encoded, _ := json.Marshal(struct {
			agentstore.Event
			Replayed bool `json:"replayed"`
		}{receipt.Event, receipt.Replayed})
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	fmt.Fprintf(stdout, "id=%s\n", receipt.Event.ID)
	if kind == agentstore.KindLaunchRequested {
		fmt.Fprintf(stdout, "request=%s\n", receipt.Event.ID)
	}
	if receipt.Replayed {
		fmt.Fprintln(stdout, "replayed=true")
	}
	return 0
}

func boolToCode(usageOnly bool) int {
	if usageOnly {
		return 2
	}
	return 0
}

// defaultBy is $HCOM_NAME (an agent) else $USER (a human at a shell).
func defaultBy() (string, string) {
	if name := strings.TrimSpace(os.Getenv("HCOM_NAME")); name != "" {
		return name, "agent"
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user, "user"
	}
	return "unknown", "unknown"
}

func usage() string {
	var b strings.Builder
	b.WriteString("herder register — record one lifecycle fact in the agent store.\n\n")
	b.WriteString("Usage:\n  herder register <kind> [--name NAME] [--by WHO] [--at RFC3339] [--id UUID] [--json] …kind flags\n\n")
	b.WriteString("Appends exactly one line to $HERDER_STATE_DIR/agents/events.jsonl under a bounded\n")
	b.WriteString("file lock. Never talks to hcom or herdr; nothing consults the store before acting.\n")
	b.WriteString("--by defaults to $HCOM_NAME, else $USER. --id makes a retry idempotent (same receipt,\n")
	b.WriteString("no second line). Exit 0 appended or replayed, 2 usage, 3 store unavailable.\n\nKinds:\n")
	for _, kind := range agentstore.Kinds {
		sp, _ := agentstore.SpecFor(kind)
		fmt.Fprintf(&b, "  %s\n", strings.TrimSpace(strings.TrimPrefix(kindUsage(kind, sp), "herder register ")))
	}
	return b.String()
}

func kindUsage(kind string, sp agentstore.Spec) string {
	var parts []string
	for _, name := range sp.Required {
		parts = append(parts, "--"+name+" V")
	}
	if len(sp.OneOf) > 0 {
		var alts []string
		for _, name := range sp.OneOf {
			alts = append(alts, "--"+name+" V")
		}
		parts = append(parts, "("+strings.Join(alts, " | ")+")")
	}
	opts := append([]string(nil), sp.Optional...)
	sort.Strings(opts)
	for _, name := range opts {
		parts = append(parts, "[--"+name+" V]")
	}
	return "herder register " + kind + " " + strings.Join(parts, " ") + "\n"
}
