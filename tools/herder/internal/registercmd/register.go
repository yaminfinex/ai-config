// Package registercmd is `herder register <kind> …`: one append to the agent
// store. The per-kind flag contract is agentstore.SpecFor; this package only
// parses flags into an Event and lets Append validate. It never talks to hcom or herdr and never blocks on anything but the
// store's bounded lock. Exit 0 append (or identical replay), 2 usage, 3 store
// unavailable.
package registercmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/eventcmd"
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
	e, asJSON, err := eventcmd.Parse(kind, args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "herder register %s: %v\n%s", kind, err, kindUsage(kind, sp))
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
	if asJSON {
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

func usage() string {
	var b strings.Builder
	b.WriteString("herder register — record one lifecycle fact in the agent store.\n\n")
	b.WriteString("Usage:\n  herder register <kind> [--name NAME] [--by WHO] [--at RFC3339] [--id UUID] [--json] …kind flags\n\n")
	b.WriteString("Appends exactly one line to $HERDER_STATE_DIR/agents/events.jsonl under a bounded\n")
	b.WriteString("file lock. Never talks to hcom or herdr; nothing consults the store before acting.\n")
	b.WriteString("--by defaults best-effort to $HCOM_NAME, else ${HCOM_TAG:+$HCOM_TAG-}$HCOM_INSTANCE_NAME, else $USER.\n")
	b.WriteString("Fleet wrappers pass --by from hcom self; the env fallback can be stale on a renamed or resumed seat.\n")
	b.WriteString("--id makes a retry idempotent (same receipt,\n")
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
		if name == "clear-group" {
			parts = append(parts, "[--clear-group]")
		} else {
			parts = append(parts, "[--"+name+" V]")
		}
	}
	return "herder register " + kind + " " + strings.Join(parts, " ") + "\n"
}
