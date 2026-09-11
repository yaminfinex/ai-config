// Package eventcmd parses the flags shared by commands that append one agent-store event.
package eventcmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"ai-config/tools/herder/internal/agentstore"
)

// Parse builds one validated event from a kind's agent-store flags. Commands
// such as register and assign own their positional syntax, usage text, append,
// and receipt; this is the single parser for their shared event flags.
func Parse(kind string, args []string) (agentstore.Event, bool, error) {
	sp, ok := agentstore.SpecFor(kind)
	if !ok {
		return agentstore.Event{}, false, fmt.Errorf("unknown kind %q", kind)
	}
	fs := flag.NewFlagSet(kind, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	values := map[string]*string{}
	boolValues := map[string]*bool{}
	for _, name := range append(append(append(append([]string(nil), agentstore.CommonFlags...), sp.Required...), sp.OneOf...), sp.Optional...) {
		if agentstore.BoolFlags[name] {
			boolValues[name] = fs.Bool(name, false, "")
			continue
		}
		if _, dup := values[name]; !dup {
			values[name] = fs.String(name, "", "")
		}
	}
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return agentstore.Event{}, false, err
	}
	if fs.NArg() > 0 {
		return agentstore.Event{}, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	get := func(name string) string {
		if value := values[name]; value != nil {
			return strings.TrimSpace(*value)
		}
		return ""
	}

	now := time.Now().UTC()
	at := now
	if text := get("at"); text != "" {
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return agentstore.Event{}, false, fmt.Errorf("--at %q is not RFC3339", text)
		}
		at = parsed.UTC()
	}
	id := get("id")
	if id == "" {
		id = agentstore.NewID(now)
	} else if !agentstore.ValidID(id) {
		return agentstore.Event{}, false, fmt.Errorf("--id %q is not UUID-shaped", id)
	}
	by, byKind := get("by"), get("by-kind")
	if by == "" {
		by, byKind = DefaultBy()
	}
	if byKind == "" {
		byKind = "agent"
	}
	e := agentstore.Event{
		ID: id, At: at, Kind: kind, By: by, ByKind: byKind, Name: get("name"), Request: get("request"),
		Tool: get("tool"), Model: get("model"), Effort: get("effort"), Tag: get("tag"), PromptRef: get("prompt-ref"),
		LauncherKind: get("launcher-kind"), Batch: get("batch"), Pane: get("pane"), Cwd: get("cwd"), Session: get("session"),
		Reason: get("reason"), Close: get("close"), FromSession: get("from-session"), FromName: get("from"),
		Group: get("group"), Title: get("title"), Note: get("note"),
		Manager: get("manager"), HcomEvent: get("hcom-event"), ParentName: get("parent-name"), Path: get("path"),
	}
	if clear := boolValues["clear-group"]; clear != nil {
		e.ClearGroup = *clear
	}
	if workspace, pane, split := get("workspace"), get("pane"), get("split-from"); kind == agentstore.KindLaunchRequested {
		e.Pane = ""
		e.Placement = &agentstore.Placement{Workspace: workspace, Pane: pane, SplitFrom: split, WorktreeBranch: get("worktree-branch"), Repo: get("repo")}
	} else if workspace != "" {
		e.Placement = &agentstore.Placement{Workspace: workspace}
	}
	if text := get("steer-chars"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil {
			return agentstore.Event{}, false, fmt.Errorf("--steer-chars %q is not an integer", text)
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
		value, err := strconv.ParseBool(text)
		if err != nil {
			return agentstore.Event{}, false, fmt.Errorf("--is-hcom-launched %q is not a bool", text)
		}
		e.IsHcomLaunched = &value
	}
	if err := e.Validate(); err != nil {
		return agentstore.Event{}, false, err
	}
	return e, *asJSON, nil
}

// DefaultBy returns the best available actor for event-producing commands.
func DefaultBy() (string, string) {
	if name := strings.TrimSpace(os.Getenv("HCOM_NAME")); name != "" {
		return name, "agent"
	}
	if instance := strings.TrimSpace(os.Getenv("HCOM_INSTANCE_NAME")); instance != "" {
		if tag := strings.TrimSpace(os.Getenv("HCOM_TAG")); tag != "" {
			return tag + "-" + instance, "agent"
		}
		return instance, "agent"
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user, "user"
	}
	return "unknown", "unknown"
}
