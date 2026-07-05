package enrollcmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help  bool
	json  bool
	label string
	role  string
}

func Run(args []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.help {
		return 0
	}
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_PANE_ID") == "" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV/HERDR_PANE_ID required)")
		return 1
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}

	paneID := os.Getenv("HERDR_PANE_ID")
	out, err := (&herdrcli.Client{}).Output("pane", "get", paneID)
	if err != nil {
		die(stderr, "herdr pane get failed for "+paneID)
		return 1
	}
	pane, err := herdrcli.ParsePaneGet(out)
	if err != nil {
		die(stderr, "could not parse herdr pane get for "+paneID)
		return 1
	}
	if pane.PaneID == "" {
		pane.PaneID = paneID
	}
	if pane.CWD == "" {
		pane.CWD, _ = os.Getwd()
	}

	guid := os.Getenv("HERDER_GUID")
	if guid == "" {
		var err error
		guid, err = registry.NewGUID()
		if err != nil {
			die(stderr, err.Error())
			return 1
		}
	}
	short := registry.ShortGUID(guid)
	label := firstNonEmpty(opts.label, os.Getenv("HERDER_LABEL"))
	if label == "" {
		label = "manual-" + short
	}
	role := firstNonEmpty(opts.role, os.Getenv("HERDER_ROLE"), "manual")

	registryPath := registry.DefaultPath()
	var recs []registry.Record
	var latest *registry.Record
	if loaded, err := registry.Load(registryPath); err == nil {
		recs = loaded
		for _, rec := range registry.LatestByGUID(recs) {
			if ptrString(rec.GUID) == guid {
				cp := rec
				latest = &cp
				break
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		die(stderr, err.Error())
		return 1
	}
	if owner := registry.ActiveLabelOwner(recs, label, guid); owner != nil {
		die(stderr, fmt.Sprintf("label %q already belongs to active guid %s", label, ptrString(owner.GUID)))
		return 1
	}

	mechanism := "enroll"
	agent := firstNonEmpty(envTool(), "manual")
	if latest != nil && latest.Provenance != nil && latest.Provenance.Mechanism != "" {
		mechanism = latest.Provenance.Mechanism
	}
	if latest != nil {
		agent = firstNonEmpty(latest.Agent, agent)
	}
	prov := registry.BuildProvenance(mechanism, os.Getenv("HCOM_TAG"), pane.CWD, pane.WorkspaceID)

	base := []byte(`{}`)
	if latest != nil && len(bytes.TrimSpace(latest.Raw)) > 0 {
		base = latest.Raw
	}
	row, err := registry.UpdateRawObject(base, map[string]any{
		"guid":         guid,
		"short_guid":   short,
		"label":        label,
		"role":         role,
		"agent":        agent,
		"pane_id":      pane.PaneID,
		"terminal_id":  pane.TerminalID,
		"workspace_id": pane.WorkspaceID,
		"cwd":          pane.CWD,
		"hcom_dir":     os.Getenv("HCOM_DIR"),
		"hcom_name":    os.Getenv("HCOM_INSTANCE_NAME"),
		"hcom_tag":     os.Getenv("HCOM_TAG"),
		"status":       "active",
		"provenance":   prov,
	})
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	if err := registry.Append(registryPath, row); err != nil {
		die(stderr, err.Error())
		return 1
	}
	fmt.Fprintf(stderr, "enrolled %s (%s) pane=%s terminal=%s\n", label, guid, pane.PaneID, pane.TerminalID)
	if opts.json {
		fmt.Fprintln(stdout, string(row))
	}
	return 0
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--label":
			if i+1 >= len(args) {
				die(stderr, "--label requires a value")
				return opts, 1
			}
			opts.label = args[i+1]
			i += 2
		case "--role":
			if i+1 >= len(args) {
				die(stderr, "--role requires a value")
				return opts, 1
			}
			opts.role = args[i+1]
			i += 2
		case "--json":
			opts.json = true
			i++
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			die(stderr, "unknown arg: "+args[i])
			return opts, 1
		}
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder enroll — register the current herdr pane in the herder registry.

Usage:
  herder enroll [--label LABEL] [--role ROLE] [--json]
`)
}

func envTool() string {
	if v := os.Getenv("HERDER_AGENT"); v != "" {
		return v
	}
	if v := os.Getenv("HCOM_TOOL"); v != "" {
		return v
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder enroll: %s\n", msg)
}
