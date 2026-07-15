// Package missioncmd implements explicit mission membership for live sessions.
package missioncmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-config/tools/herder/internal/missioncontext"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type options struct {
	help   bool
	target string
	slug   string
}

type refusal struct {
	cause  string
	reason string
	remedy string
}

func (r *refusal) Error() string { return r.reason }

type mutationResult struct {
	guid     string
	label    string
	previous string
	applied  bool
}

func RunJoin(args []string, stdout, stderr io.Writer) int {
	opts, code := parse("join", args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	mission, err := missioncontext.ResolveExplicit(opts.slug, missioncontext.Options{})
	if err != nil {
		return dieRefusal(stderr, "join", contextRefusal(err))
	}
	result, err := mutate(opts.target, &mission)
	if err != nil {
		return dieRefusal(stderr, "join", asRefusal(err))
	}
	name := displayTarget(result)
	if result.applied {
		fmt.Fprintf(stdout, "joined %s to mission %s\n", name, mission.Slug)
	} else {
		fmt.Fprintf(stdout, "%s is already joined to mission %s; no registry row appended\n", name, mission.Slug)
	}
	return 0
}

func RunLeave(args []string, stdout, stderr io.Writer) int {
	opts, code := parse("leave", args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}
	result, err := mutate(opts.target, nil)
	if err != nil {
		return dieRefusal(stderr, "leave", asRefusal(err))
	}
	name := displayTarget(result)
	if result.applied {
		fmt.Fprintf(stdout, "left mission %s for %s; cwd inference is active\n", result.previous, name)
	} else {
		fmt.Fprintf(stdout, "%s has no explicit mission membership; cwd inference is already active\n", name)
	}
	return 0
}

func mutate(explicitTarget string, mission *v2.Mission) (mutationResult, error) {
	target := explicitTarget
	if target == "" {
		target = os.Getenv("HERDER_GUID")
	}
	if target == "" {
		return mutationResult{}, &refusal{
			cause:  "caller_identity_missing",
			reason: "neither --target nor HERDER_GUID identifies a session",
			remedy: "run inside an enrolled session or pass --target",
		}
	}
	path := registry.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mutationResult{}, &refusal{
				cause:  "session_not_found",
				reason: fmt.Sprintf("target %q has no registry row", target),
				remedy: "check `herder list --json` and retry with its guid or label",
			}
		}
		return mutationResult{}, err
	}

	var result mutationResult
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2Resolve(tx.Projection, target)
		if current == nil {
			return nil, &refusal{
				cause:  "session_not_found",
				reason: fmt.Sprintf("target %q has no registry row", target),
				remedy: "check `herder list --json` and retry with its guid or label",
			}
		}
		result.guid, result.label = current.GUID, current.Label
		if current.State != v2.StateSeated || current.Seat == nil {
			return nil, &refusal{
				cause:  "session_not_live",
				reason: fmt.Sprintf("target %s is %s, not a live seated session", current.GUID, current.State),
				remedy: "start or resume the agent, then retry",
			}
		}
		if mission != nil {
			if current.Mission != nil && current.Mission.Slug == mission.Slug {
				return nil, nil
			}
			if current.Mission != nil {
				return nil, &refusal{
					cause:  "already_joined",
					reason: fmt.Sprintf("target %s is already joined to mission %s", current.GUID, current.Mission.Slug),
					remedy: "run `herder leave` for the target, then join the new mission",
				}
			}
			next := *current
			next.Event = "mission_joined"
			next.RecordedAt = ""
			next.Mission = &v2.Mission{Slug: mission.Slug, Source: missioncontext.SourceExplicit}
			result.applied = true
			return []v2.SessionRecord{next}, nil
		}
		if current.Mission == nil {
			return nil, nil
		}
		result.previous = current.Mission.Slug
		next := *current
		next.Event = "mission_left"
		next.RecordedAt = ""
		next.Mission = nil
		result.applied = true
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return mutationResult{}, err
	}
	if !result.applied {
		return result, nil
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		return mutationResult{}, err
	}
	if err := outcome.Err(); err != nil {
		return mutationResult{}, err
	}
	if outcome.Status != registry.WriteApplied {
		return mutationResult{}, fmt.Errorf("registry returned %s for mission mutation", outcome.Status)
	}
	return result, nil
}

func parse(verb string, args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	var positional []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-h" || arg == "--help":
			printHelp(verb, stdout)
			opts.help = true
			return opts, 0
		case arg == "--target":
			if i+1 >= len(args) || args[i+1] == "" {
				return opts, usage(stderr, verb, "--target requires a value")
			}
			i++
			opts.target = args[i]
		case strings.HasPrefix(arg, "--target="):
			opts.target = strings.TrimPrefix(arg, "--target=")
			if opts.target == "" {
				return opts, usage(stderr, verb, "--target requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return opts, usage(stderr, verb, "unknown option: "+arg)
		default:
			positional = append(positional, arg)
		}
	}
	if verb == "join" {
		if len(positional) != 1 {
			return opts, usage(stderr, verb, "expected exactly one mission slug")
		}
		opts.slug = positional[0]
	} else if len(positional) != 0 {
		return opts, usage(stderr, verb, "leave accepts no positional arguments")
	}
	return opts, 0
}

func printHelp(verb string, stdout io.Writer) {
	if verb == "join" {
		fmt.Fprint(stdout, `herder join — declare explicit mission membership for a running agent.

Usage:
  herder join <mission-slug> [--target <guid|short-guid|label|pane>]

Without --target, HERDER_GUID identifies the caller. The target must be seated.
An explicit membership overrides cwd and .mission inference in list --json.
`)
		return
	}
	fmt.Fprint(stdout, `herder leave — remove explicit mission membership from a running agent.

Usage:
  herder leave [--target <guid|short-guid|label|pane>]

Without --target, HERDER_GUID identifies the caller. Leaving removes the
explicit value so list --json returns to cwd and .mission inference.
`)
}

func usage(stderr io.Writer, verb, message string) int {
	fmt.Fprintf(stderr, "herder %s: %s — run `herder %s --help` for usage\n", verb, message, verb)
	return 2
}

func contextRefusal(err error) *refusal {
	var source *missioncontext.Refusal
	if errors.As(err, &source) {
		return &refusal{cause: source.Kind, reason: source.Reason, remedy: source.Remedy}
	}
	return &refusal{cause: "mission_lookup_failed", reason: err.Error(), remedy: "fix the mission repository and retry"}
}

func asRefusal(err error) *refusal {
	var typed *refusal
	if errors.As(err, &typed) {
		return typed
	}
	return &refusal{cause: "registry_write_failed", reason: err.Error(), remedy: "inspect the registry and retry after correcting the write failure"}
}

func dieRefusal(stderr io.Writer, verb string, r *refusal) int {
	fmt.Fprintf(stderr, "herder %s: refused [%s]: %s — remedy: %s\n", verb, r.cause, r.reason, r.remedy)
	return 1
}

func displayTarget(result mutationResult) string {
	if result.label != "" {
		return result.label + " (" + result.guid + ")"
	}
	return result.guid
}
