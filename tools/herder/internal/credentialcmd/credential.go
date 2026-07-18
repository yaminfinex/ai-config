// Package credentialcmd exposes non-secret credential discovery and the
// one-time live issuance sweep used before credential-authenticated cutover.
package credentialcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/seatcred"
)

type sweepReport struct {
	Total    int      `json:"total"`
	Covered  int      `json:"covered"`
	Issued   int      `json:"issued"`
	Blockers []string `json:"blockers,omitempty"`
}

// Run dispatches `herder credential path|sweep`.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "path":
		return runPath(args[1:], stdout, stderr)
	case "sweep":
		return runSweep(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herder credential: unknown operation %q\n", args[0])
		return 2
	}
}

func runPath(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("credential path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	guid := fs.String("guid", "", "seated guid")
	if err := fs.Parse(args); err != nil || *guid == "" || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stderr, "herder credential path: --guid GUID is required")
		}
		return 2
	}
	path, err := seatcred.CurrentPath(registry.DefaultPath(), *guid)
	if err != nil {
		fmt.Fprintf(stderr, "herder credential path: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

func runSweep(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("credential sweep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "emit JSON report")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	path := registry.DefaultPath()
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "herder credential sweep: registry does not exist")
		} else {
			fmt.Fprintf(stderr, "herder credential sweep: load registry: %v\n", err)
		}
		return 1
	}
	report := sweepReport{}
	for _, row := range seatedRows(projection) {
		report.Total++
		if row.Seat.CredentialGeneration != "" {
			credentialPath := seatcred.CredentialPath(path, row.GUID, row.Seat.CredentialGeneration)
			if _, err := seatcred.Authenticate(path, credentialPath); err != nil {
				report.Blockers = append(report.Blockers, fmt.Sprintf("%s: current credential unavailable (%v); run `herder repair reissue-credential --guid %s`", row.GUID, err, row.GUID))
			} else {
				report.Covered++
			}
			continue
		}
		completed, err := seatcompletion.Complete(context.Background(), completionRequest(path, row))
		if err != nil {
			report.Blockers = append(report.Blockers, fmt.Sprintf("%s: %v", row.GUID, err))
			continue
		}
		if completed.Refusal != nil {
			report.Blockers = append(report.Blockers, fmt.Sprintf("%s: [%s] %s", row.GUID, completed.Refusal.Code, completed.Refusal.Cause))
			continue
		}
		if completed.CredentialGeneration == "" {
			report.Blockers = append(report.Blockers, fmt.Sprintf("%s: completion did not commit a credential generation", row.GUID))
			continue
		}
		report.Covered++
		report.Issued++
		fmt.Fprintf(stderr, "credential issued guid=%s generation=%s path=%s\n", row.GUID, completed.CredentialGeneration, completed.CredentialPath)
	}
	if *jsonOutput {
		_ = json.NewEncoder(stdout).Encode(report)
	} else {
		fmt.Fprintf(stdout, "credential coverage: %d/%d", report.Covered, report.Total)
		if report.Total == 0 || report.Covered == report.Total {
			fmt.Fprintln(stdout, " (100%)")
		} else {
			fmt.Fprintln(stdout)
		}
		for _, blocker := range report.Blockers {
			fmt.Fprintf(stdout, "blocker: %s\n", blocker)
		}
	}
	if report.Covered != report.Total {
		fmt.Fprintln(stderr, "herder credential sweep: cutover refused until every currently seated row is covered")
		return 1
	}
	if err := seatcred.EnableCutover(path); err != nil {
		fmt.Fprintf(stderr, "herder credential sweep: coverage is complete but cutover marker could not be committed: %v\n", err)
		return 1
	}
	return 0
}

func seatedRows(projection *v2.Projection) []v2.SessionRecord {
	rows := make([]v2.SessionRecord, 0)
	for _, row := range projection.Sessions() {
		if row.State == v2.StateSeated && row.Seat != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func completionRequest(path string, row v2.SessionRecord) seatcompletion.Request {
	seat := row.Seat
	claim := seatcompletion.SeatClaim{Kind: seat.Kind, PaneID: seat.PaneID, TerminalID: seat.TerminalID, PID: seat.PID}
	evidence := hcomidentity.Evidence{Name: seat.HcomName, PaneIDs: []string{seat.PaneID}}
	return seatcompletion.Request{
		Origin: seatcompletion.OriginRecognition, RegistryPath: path, CredentialGUID: row.GUID,
		Candidate: row, Seat: claim, Namespace: seat.Namespace, Evidence: evidence,
		RequireBus: seat.HcomName != "",
		FinalizeLocked: func(_ registry.LockedUpdate, current *v2.SessionRecord, _ *v2.SessionRecord, _ string) error {
			if current == nil || current.GUID != row.GUID || current.State != v2.StateSeated || current.Seat == nil {
				return fmt.Errorf("seat changed during sweep")
			}
			if current.Seat.CredentialGeneration != "" {
				return fmt.Errorf("seat gained a credential generation during sweep; rerun to verify it")
			}
			if current.Seat.Kind != seat.Kind || current.Seat.PaneID != seat.PaneID || current.Seat.TerminalID != seat.TerminalID || current.Seat.PID != seat.PID || current.Seat.HcomName != seat.HcomName {
				return fmt.Errorf("seat coordinates changed during sweep")
			}
			return nil
		},
	}
}

const usage = `usage:
  herder credential path --guid GUID
  herder credential sweep [--json]

path prints only the registry-derived path for the current generation. sweep
issues credentials for live legacy seats and succeeds only at 100% coverage.
`
