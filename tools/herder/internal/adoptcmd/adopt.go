// Package adoptcmd composes the existing identity lifecycle verbs for a
// restarted process. The replacement receives a fresh guid; no transcript is
// ever re-keyed onto the prior session's guid.
package adoptcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"ai-config/tools/herder/internal/enrollcmd"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/renamecmd"
	"ai-config/tools/herder/internal/retirecmd"
	"ai-config/tools/herder/internal/shellquote"
)

type options struct {
	help                  bool
	target                string
	confirmDead           bool
	confirmResumedSession bool
}

func Run(args []string, stdout, stderr io.Writer) int {
	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 || opts.help {
		return code
	}

	old, err := loadOld(opts.target)
	if err != nil {
		die(stderr, err.Error())
		return 1
	}
	if old.State == v2.StateLost {
		die(stderr, fmt.Sprintf("old target %s is lost; LOST sessions cannot transfer labels, so adoption cannot proceed", old.GUID))
		return 1
	}
	if old.State == v2.StateRetired {
		die(stderr, fmt.Sprintf("old target %s is retired and already released its label; enroll the replacement, then use plain rename", old.GUID))
		return 1
	}
	if old.Label == "" {
		die(stderr, fmt.Sprintf("old target %s has no label to adopt; use 'herder enroll' for the replacement", old.GUID))
		return 1
	}
	oldBus, oldBusDir := busCoordinates(old)
	priorSessionID := latestSessionID(old)
	unseatReason := ""
	expectedSourcePane := ""
	if old.State == v2.StateSeated {
		oldPane := ""
		if old.Seat != nil {
			oldPane = old.Seat.PaneID
		}
		expectedSourcePane = oldPane
		liveCaller := hcomidentity.ResolveLive(oldBusDir, hcomidentity.CurrentEvidence(os.Getenv("HERDR_PANE_ID")))
		var authErr error
		unseatReason, authErr = adoptionUnseatReason(oldPane, liveCaller, opts.confirmDead)
		if authErr != nil {
			die(stderr, fmt.Sprintf("old target %s is seated on pane %s, but the caller's own pane is not proven to be the same (%s); refusing before enrollment so no replacement row is created. If the old transcript is dead, rerun 'herder adopt %s --confirm-dead'", old.GUID, displayPane(oldPane), authErr, old.GUID))
			return 1
		}
	}
	if err := preflightRecordedSessionClaim(oldBusDir, priorSessionID, os.Getenv("HERDR_PANE_ID"), opts.confirmResumedSession); err != nil {
		die(stderr, fmt.Sprintf("recorded resumed-session preflight refused: %s; refusing before enrollment", err))
		return 1
	}
	enrollArgs := []string{"--json"}
	if old.Role != "" {
		enrollArgs = append(enrollArgs, "--role", old.Role)
	}
	var enrollOut, enrollErr bytes.Buffer
	if rc := enrollcmd.RunFreshForAdoption(enrollArgs, &enrollOut, &enrollErr, old.GUID); rc != 0 {
		die(stderr, "enroll leg refused: "+oneLine(enrollErr.String()))
		return 1
	}
	var replacement v2.SessionRecord
	if err := json.Unmarshal(bytes.TrimSpace(enrollOut.Bytes()), &replacement); err != nil || replacement.GUID == "" {
		cause := "enroll returned no parseable applied row"
		if err != nil {
			cause += ": " + err.Error()
		}
		die(stderr, cause+"; inspect the registry with 'herder list --all --json' before retrying")
		return 1
	}
	if replacement.GUID == old.GUID {
		die(stderr, fmt.Sprintf("enroll leg illegally reused old guid %s; adoption stopped before label transfer. Enroll the replacement under a fresh guid, then run 'herder rename <new-guid> --take-from %s'", old.GUID, old.GUID))
		return 1
	}
	fmt.Fprintf(stderr, "adopt: enroll applied: new guid %s seated as %s\n", replacement.GUID, replacement.Label)
	forwardWarnings(stderr, enrollErr.String())

	result, transferErr := transferForAdoption(registry.DefaultPath(), replacement.GUID, old.GUID, expectedSourcePane, unseatReason)
	if transferErr != nil {
		failureAfter(stderr, "label-transfer", transferErr.Error(),
			[]string{"enroll applied for new guid " + replacement.GUID},
			[]string{
				fmt.Sprintf("herder rename %s --take-from %s", replacement.GUID, old.GUID),
				fmt.Sprintf("herder retire %s", old.GUID),
				busRecovery(oldBus, oldBusDir),
			})
		return 1
	}
	renamecmd.SyncHerdrName(result.TargetTerminalID, result.Label, stdout)
	fmt.Fprintf(stderr, "adopt: label-transfer applied: %s now labels guid %s\n", old.Label, replacement.GUID)

	var retireOut, retireErr bytes.Buffer
	if rc := retirecmd.RunRetire([]string{old.GUID}, &retireOut, &retireErr); rc != 0 {
		failureAfter(stderr, "retire", oneLine(retireErr.String()),
			[]string{
				"enroll applied for new guid " + replacement.GUID,
				"label-transfer applied for label " + old.Label,
			},
			[]string{
				fmt.Sprintf("herder retire %s", old.GUID),
				busRecovery(oldBus, oldBusDir),
			})
		return 1
	}
	_, _ = io.Copy(stdout, &retireOut)
	fmt.Fprintf(stderr, "adopt: retire applied: old guid %s retired\n", old.GUID)

	busIdentity, err := reclaimOrVerifyBus(oldBus, oldBusDir, priorSessionID, os.Getenv("HERDR_PANE_ID"), opts.confirmResumedSession)
	if err != nil {
		failureAfter(stderr, "bus-name", err.Error(),
			[]string{
				"enroll applied for new guid " + replacement.GUID,
				"label-transfer applied for label " + old.Label,
				"retire applied for old guid " + old.GUID,
			},
			[]string{busRecovery(oldBus, oldBusDir), "herder enroll"})
		return 1
	}
	fmt.Fprintf(stderr, "adopt: bus-name verified: @%s belongs to the replacement session\n", busIdentity.Name)
	busDisposition := "reclaimed"
	if busIdentity.Name != oldBus {
		busDisposition = "ADOPTED as @" + busIdentity.Name
		fmt.Fprintf(stderr, "adopt: bus-name note: requested @%s was not reclaimed; ADOPTED already-live @%s from the confirmed resumed transcript session\n", oldBus, busIdentity.Name)
	}
	if err := bindReplacementBus(registry.DefaultPath(), replacement.GUID, oldBusDir, busIdentity); err != nil {
		failureAfter(stderr, "registry-bind", err.Error(),
			[]string{
				"enroll applied for new guid " + replacement.GUID,
				"label-transfer applied for label " + old.Label,
				"retire applied for old guid " + old.GUID,
				"bus-name verified as @" + busIdentity.Name,
			},
			[]string{pinnedReEnroll(replacement, busIdentity.SessionID)})
		return 1
	}
	fmt.Fprintf(stderr, "adopt: registry-bind applied: @%s recorded on guid %s\n", busIdentity.Name, replacement.GUID)
	fmt.Fprintf(stderr, "adopted %s: new guid %s seated; old guid %s retired; label reclaimed; bus identity %s\n", old.Label, replacement.GUID, old.GUID, busDisposition)
	return 0
}

func adoptionUnseatReason(oldPane string, caller hcomidentity.Result, confirmDead bool) (string, error) {
	if confirmDead {
		return renamecmd.AdoptionReasonConfirmedDead, nil
	}
	if !caller.Verified {
		return "", fmt.Errorf("live caller identity is unverified: %s", caller.Reason)
	}
	if caller.PaneID == "" {
		return "", fmt.Errorf("live caller identity has no pane coordinate")
	}
	if oldPane == "" {
		return "", fmt.Errorf("old seated row has no pane coordinate; caller pane is %s", caller.PaneID)
	}
	if caller.PaneID != oldPane {
		return "", fmt.Errorf("caller occupies pane %s", caller.PaneID)
	}
	return renamecmd.AdoptionReasonSeatSuperseded, nil
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	var opts options
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
			return opts, 0
		case "--confirm-dead":
			if opts.confirmDead {
				die(stderr, "--confirm-dead may be specified only once")
				return opts, 1
			}
			opts.confirmDead = true
		case "--confirm-resumed-session":
			if opts.confirmResumedSession {
				die(stderr, "--confirm-resumed-session may be specified only once")
				return opts, 1
			}
			opts.confirmResumedSession = true
		default:
			if opts.target != "" {
				die(stderr, "unexpected arg: "+arg)
				return opts, 1
			}
			opts.target = arg
		}
	}
	if opts.target == "" {
		die(stderr, "usage: herder adopt <old-target> [--confirm-dead] [--confirm-resumed-session]")
		return opts, 1
	}
	return opts, 0
}

func loadOld(target string) (v2.SessionRecord, error) {
	path := registry.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return v2.SessionRecord{}, fmt.Errorf("no registry at %s", path)
		}
		return v2.SessionRecord{}, err
	}
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		return v2.SessionRecord{}, err
	}
	rec := registry.V2Resolve(proj, target)
	if rec == nil {
		return v2.SessionRecord{}, fmt.Errorf("unknown old target: %s", target)
	}
	return *rec, nil
}

func transferForAdoption(path, target, source, expectedSourcePane, unseatReason string) (renamecmd.TransferResult, error) {
	if unseatReason != "" {
		return renamecmd.TransferForAdoption(path, target, source, expectedSourcePane, unseatReason)
	}
	return renamecmd.Transfer(path, target, source, false)
}

func displayPane(pane string) string {
	if pane == "" {
		return "<unknown>"
	}
	return pane
}

func busCoordinates(rec v2.SessionRecord) (string, string) {
	name := ""
	dir := ""
	if rec.Seat != nil {
		name = rec.Seat.HcomName
		dir = rec.Seat.Namespace
	} else if legacy, ok := registry.DecodeLegacyV1Raw(rec); ok {
		name = legacy.HcomName
		dir = legacy.HcomDir
	}
	if name == "" {
		// Labels and bus names are normally aligned. An unseated row has released
		// its seat coordinates, so the durable label is the only reclaim key left.
		name = rec.Label
	}
	if dir == "" {
		dir = os.Getenv("HCOM_DIR")
	}
	return name, dir
}

func reclaimOrVerifyBus(want, dir, priorSessionID, callerPane string, confirmResumedSession bool) (hcomidentity.Result, error) {
	evidence := hcomidentity.CurrentEvidence(callerPane)
	rows, err := hcomidentity.List(dir)
	if err != nil {
		return hcomidentity.Result{}, fmt.Errorf("cannot inspect the live bus roster (%v); run %s, then 'herder enroll' to verify the binding", err, busRecovery(want, dir))
	}
	resolved := hcomidentity.Resolve(rows, evidence)
	// A hand-recovered resume often lacks every launch-time hcom variable while
	// its hooks have already rejoined the bus row under the transcript session id
	// preserved by the source registry row. The SID identifies the candidate;
	// caller-pane agreement or an explicit pane-less assertion authorizes the
	// caller to adopt it without minting a placeholder bus row.
	if !resolved.Verified && priorSessionID != "" {
		resumed := hcomidentity.Resolve(rows, hcomidentity.Evidence{SessionID: priorSessionID})
		if resumed.Verified {
			if err := authorizeRecordedSessionClaim(resumed, callerPane, confirmResumedSession); err != nil {
				return hcomidentity.Result{}, err
			}
			return resumed, nil
		}
	}
	if want == "" {
		if resolved.Verified {
			return resolved, nil
		}
		return hcomidentity.Result{}, fmt.Errorf("old row has no bus name and the replacement bus identity is unverified (%s); join hcom, then run 'herder enroll'", resolved.Reason)
	}
	if ok, verified := hcomidentity.VerifyStored(rows, evidence, want); ok {
		return verified, nil
	}
	if held, ok := hcomidentity.JoinedNamed(rows, want); ok {
		holder := held.SessionID
		if holder == "" {
			holder = held.Name
		}
		return hcomidentity.Result{}, fmt.Errorf("@%s is held by a live different session (%s); refusing to steal it. Verify that session with 'hcom list %s', then reclaim only after it has released the name", want, holder, want)
	}

	cmd := exec.Command("hcom", "start", "--as", want)
	cmd.Env = os.Environ()
	if dir != "" && dir != "null" {
		cmd.Env = setEnv(cmd.Env, "HCOM_DIR", dir)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return hcomidentity.Result{}, fmt.Errorf("hcom reclaim failed: %s; run %s, then 'herder enroll'", commandCause(out, err), busRecovery(want, dir))
	}
	rows, err = hcomidentity.List(dir)
	if err != nil {
		return hcomidentity.Result{}, fmt.Errorf("hcom reclaim ran but verification failed: %v; run 'herder enroll' to verify the binding", err)
	}
	if ok, verified := hcomidentity.VerifyStored(rows, evidence, want); ok {
		return verified, nil
	}
	// The requested name was proven unheld immediately before this process ran
	// `hcom start --as`, and that command succeeded. Seeing the same name joined
	// afterward is operation-scoped ownership proof even when a hand-resumed
	// process carries none of hcom's launch-time session/process/pane correlates.
	// This proof is intentionally local to adoption; general identity resolution
	// must keep refusing cwd/tag/name guesses.
	joined, joinedCount := hcomidentity.JoinedNamedCount(rows, want)
	if joinedCount == 1 {
		return hcomidentity.Result{
			Name:      joined.Name,
			SessionID: joined.SessionID,
			PaneID:    joined.LaunchContext.PaneID,
			Verified:  true,
		}, nil
	}
	if joinedCount > 1 {
		return hcomidentity.Result{}, fmt.Errorf("hcom reclaim ran but @%s matches multiple joined bus rows; refusing ambiguous operation-scoped ownership proof", want)
	}
	return hcomidentity.Result{}, fmt.Errorf("hcom reclaim ran but @%s is not joined; run 'herder enroll' after the live bus row appears", want)
}

// preflightRecordedSessionClaim keeps an unproven source SID from reaching
// adoption's mutating legs. The same authorization is repeated after reclaim
// because the live roster can change between preflight and persistence.
func preflightRecordedSessionClaim(dir, priorSessionID, callerPane string, confirmResumedSession bool) error {
	if priorSessionID == "" {
		return nil
	}
	rows, err := hcomidentity.List(dir)
	if err != nil {
		return fmt.Errorf("cannot inspect the live bus roster: %w", err)
	}
	if hcomidentity.Resolve(rows, hcomidentity.CurrentEvidence(callerPane)).Verified {
		return nil
	}
	resumed := hcomidentity.Resolve(rows, hcomidentity.Evidence{SessionID: priorSessionID})
	if !resumed.Verified {
		return nil
	}
	return authorizeRecordedSessionClaim(resumed, callerPane, confirmResumedSession)
}

func authorizeRecordedSessionClaim(resumed hcomidentity.Result, callerPane string, confirmResumedSession bool) error {
	if resumed.PaneID != "" {
		if callerPane != resumed.PaneID {
			return fmt.Errorf("source transcript session id resolves to live @%s on pane %s, but the caller pane is %s", resumed.Name, resumed.PaneID, displayPane(callerPane))
		}
		return nil
	}
	if !confirmResumedSession {
		return fmt.Errorf("source transcript session id resolves to live @%s without a launch pane; --confirm-resumed-session is required to assert that the caller owns that hand-resumed transcript", resumed.Name)
	}
	return nil
}

func latestSessionID(rec v2.SessionRecord) string {
	if len(rec.SIDs) > 0 {
		return rec.SIDs[len(rec.SIDs)-1].SID
	}
	return rec.Provenance.ToolSessionID
}

// bindReplacementBus closes adoption's final persistence gap: reclaiming a
// live bus name is not enough when every later delivery verb resolves through
// the registry row. The replacement guid is already known exactly, so this
// append never guesses an identity from a display coordinate.
func bindReplacementBus(path, guid, dir string, identity hcomidentity.Result) error {
	if !identity.Verified || identity.Name == "" {
		return errors.New("replacement bus identity is not verified")
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, guid)
		if current == nil {
			return nil, fmt.Errorf("replacement guid %s no longer exists", guid)
		}
		if current.State != v2.StateSeated || current.Seat == nil {
			return nil, fmt.Errorf("replacement guid %s is no longer seated", guid)
		}
		next := *current
		next.SIDs = append([]v2.SID(nil), current.SIDs...)
		next.Event = "recognised"
		next.RecordedAt = stamp
		next.ObservedVia = "adopt bus reclaim"
		seat := *current.Seat
		seat.HcomName = identity.Name
		verified := true
		seat.HcomVerified = &verified
		if dir != "" && dir != "null" {
			seat.Namespace = dir
		}
		seat.ConfirmedAt = stamp
		next.Seat = &seat
		if identity.SessionID != "" {
			next.Provenance.ToolSessionID = identity.SessionID
			if !hasSID(next.SIDs, identity.SessionID) {
				next.SIDs = append(next.SIDs, v2.SID{SID: identity.SessionID, ObservedAt: stamp, Source: "adopt bus reclaim"})
			}
			next.Continuity = "confirmed"
		}
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		return err
	}
	if len(outcomes) != 1 {
		return fmt.Errorf("registry returned %d outcomes for the replacement binding", len(outcomes))
	}
	return outcomes[0].Err()
}

func hasSID(sids []v2.SID, want string) bool {
	for _, sid := range sids {
		if sid.SID == want {
			return true
		}
	}
	return false
}

func pinnedReEnroll(rec v2.SessionRecord, sessionID string) string {
	sessionWord := ""
	if sessionID == "" {
		sessionWord = "'<resumed-session-id>'"
	} else {
		sessionWord = shellquote.Quote(sessionID)
	}
	return fmt.Sprintf("HCOM_SESSION_ID=%s HERDER_GUID=%s HERDER_LABEL=%s herder enroll",
		sessionWord, shellquote.Quote(rec.GUID), shellquote.Quote(rec.Label))
}

func failureAfter(stderr io.Writer, leg, cause string, applied, remaining []string) {
	fmt.Fprintf(stderr, "herder adopt: %s leg failed: %s\n", leg, cause)
	for _, item := range applied {
		fmt.Fprintf(stderr, "  applied: %s\n", item)
	}
	fmt.Fprintln(stderr, "  remaining manual steps:")
	for _, step := range remaining {
		if step != "" {
			fmt.Fprintf(stderr, "    %s\n", step)
		}
	}
}

func busRecovery(name, dir string) string {
	if name == "" {
		return "hcom start"
	}
	command := fmt.Sprintf("hcom start --as %s", name)
	if dir != "" && dir != "null" {
		command = fmt.Sprintf("HCOM_DIR=%s %s", dir, command)
	}
	return command
}

func commandCause(out []byte, err error) string {
	if text := oneLine(string(out)); text != "" {
		return text
	}
	return err.Error()
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func forwardWarnings(stderr io.Writer, value string) {
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if line != "" && !strings.HasPrefix(line, "enrolled ") {
			fmt.Fprintf(stderr, "adopt: enroll note: %s\n", line)
		}
	}
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder adopt — replace a restarted session without reusing its guid.

Usage:
  herder adopt <old-target> [--confirm-dead] [--confirm-resumed-session]

Run inside the replacement's live herdr pane. Adopt composes five explicit
legs: enroll the replacement under a NEW guid, atomically take the old row's
label, retire the old row, reclaim or verify its hcom bus identity, then record
that verified identity on the replacement row. A restart is a new transcript,
so the old guid is never moved or re-keyed.

For a hand-resumed transcript that lacks ambient hcom identity variables,
adopt can verify an already-live bus row from the source row's recorded tool
session id only when its live launch pane matches the caller. If that roster
row has no launch pane, --confirm-resumed-session explicitly asserts that the
caller owns the source transcript. It never guesses bus ownership from a name,
tag, or directory.

A seated old target in the caller's own pane is provably superseded: adopt
atomically unseats it while moving its label, recording "seat superseded by
replacement process in the same pane" as the reason. A seated target on a
different pane refuses before enrollment unless --confirm-dead asserts that
the old transcript is dead; that path records "operator confirmed old
transcript dead". Plain rename --confirm-live has different semantics and is
never used by adopt. Lost and retired old targets still refuse.

If a later leg fails, adopt reports every applied leg and the exact remaining
manual commands. It does not roll back an applied enrollment. A bus name held
by a different live session is never stolen.
`)
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder adopt: %s\n", msg)
}
