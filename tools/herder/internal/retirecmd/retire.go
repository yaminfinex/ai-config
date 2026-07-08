package retirecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type options struct {
	help bool
	json bool
}

func RunRetire(args []string, stdout, stderr io.Writer) int {
	opts, target, code := parseArgs("retire", args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	registryPath := registry.DefaultPath()
	if _, err := os.Stat(registryPath); err != nil && errors.Is(err, os.ErrNotExist) {
		die(stderr, "retire", "no registry at "+registryPath)
		return 1
	}

	var guid, oldLabel string
	encoded, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rec := registry.V2Resolve(tx.Projection, target)
		if rec == nil {
			return nil, fmt.Errorf("unknown target: %s", target)
		}
		guid = rec.GUID
		oldLabel = rec.Label
		switch rec.State {
		case v2.StateRetired:
			return nil, nil
		case v2.StateSeated:
			return nil, fmt.Errorf("target %s is seated; cull first, then retire the unseated session", rec.GUID)
		case v2.StateLost:
			return nil, fmt.Errorf("target %s is lost; LOST sessions cannot be retired", rec.GUID)
		case v2.StateUnseated:
			if rec.Seat != nil {
				return nil, fmt.Errorf("target %s is unseated but still has a seat; refusing anomalous row", rec.GUID)
			}
			next := *rec
			next.Event = "retired"
			next.RecordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
			next.State = v2.StateRetired
			next.Label = ""
			next.Seat = nil
			return []v2.SessionRecord{next}, nil
		default:
			return nil, fmt.Errorf("target %s has unknown state %q; refusing retire", rec.GUID, rec.State)
		}
	})
	if err != nil {
		die(stderr, "retire", err.Error())
		return 1
	}
	row := transitionRow(encoded, guid, "retired")
	if row == nil {
		fmt.Fprintf(stderr, "retired %s already retired (%s); no registry row appended\n", displayLabel(oldLabel), guid)
		return 0
	}
	if opts.json {
		fmt.Fprintln(stdout, string(row))
	}
	fmt.Fprintf(stderr, "retired %s (%s); label released\n", displayLabel(oldLabel), guid)
	return 0
}

func RunReopen(args []string, stdout, stderr io.Writer) int {
	opts, target, code := parseArgs("reopen", args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	registryPath := registry.DefaultPath()
	if _, err := os.Stat(registryPath); err != nil && errors.Is(err, os.ErrNotExist) {
		die(stderr, "reopen", "no registry at "+registryPath)
		return 1
	}

	var guid string
	encoded, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		rec := registry.V2Resolve(tx.Projection, target)
		if rec == nil {
			return nil, fmt.Errorf("unknown target: %s", target)
		}
		guid = rec.GUID
		if rec.State != v2.StateRetired {
			return nil, fmt.Errorf("target %s is %s, not retired; reopen only accepts retired sessions", rec.GUID, rec.State)
		}
		next := *rec
		next.Event = "reopened"
		next.RecordedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
		next.State = v2.StateUnseated
		next.Label = ""
		next.Seat = nil
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		die(stderr, "reopen", err.Error())
		return 1
	}
	row := transitionRow(encoded, guid, "reopened")
	if row == nil {
		die(stderr, "reopen", fmt.Sprintf("target %s was not reopened", target))
		return 1
	}
	if opts.json {
		fmt.Fprintln(stdout, string(row))
	}
	fmt.Fprintf(stderr, "reopened %s unseated and unlabelled\n", guid)
	return 0
}

func parseArgs(verb string, args []string, stdout, stderr io.Writer) (options, string, int) {
	var opts options
	var target string
	for i := 0; i < len(args); {
		switch args[i] {
		case "--json":
			opts.json = true
			i++
		case "-h", "--help":
			printHelp(verb, stdout)
			opts.help = true
			return opts, "", 0
		default:
			if target != "" {
				die(stderr, verb, "usage: herder "+verb+" <target> [--json]")
				return opts, "", 1
			}
			target = args[i]
			i++
		}
	}
	if target == "" {
		die(stderr, verb, "usage: herder "+verb+" <target> [--json]")
		return opts, "", 1
	}
	return opts, target, 0
}

func printHelp(verb string, stdout io.Writer) {
	if verb == "reopen" {
		fmt.Fprint(stdout, `herder reopen — move a retired session back to unseated, without a label.

Usage:
  herder reopen <target> [--json]

<target> is a short-guid, full guid, label, or pane_id. Only retired sessions
can be reopened. The reopened session is always unseated and unlabelled; claim
a label afterwards with 'herder rename'.
`)
		return
	}
	fmt.Fprint(stdout, `herder retire — explicitly close an unseated session for good and release its label.

Usage:
  herder retire <target> [--json]

<target> is a short-guid, full guid, label, or pane_id. Retire is legal only
from unseated sessions. If the target is seated, cull first, then retire the
unseated session. Lost sessions cannot be retired. Retiring an already-retired
session succeeds as a confirmed no-op and appends no duplicate row.
`)
}

func displayLabel(label string) string {
	if label == "" {
		return "<unlabelled>"
	}
	b, err := json.Marshal(label)
	if err != nil {
		return label
	}
	return string(b)
}

func transitionRow(rows [][]byte, guid, event string) []byte {
	for _, row := range rows {
		var rec v2.SessionRecord
		if err := json.Unmarshal(row, &rec); err != nil {
			continue
		}
		if rec.GUID == guid && rec.Event == event {
			return row
		}
	}
	return nil
}

func die(stderr io.Writer, verb, msg string) {
	fmt.Fprintf(stderr, "herder %s: %s\n", verb, msg)
}
