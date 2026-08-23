package spawncmd

// This file is the remaining IMPL-3 sender-fence compatibility seam. Compact
// no longer calls it: compact resolves through occupant.SelfProbe. Spawn's
// prompt sender replaces this fossil ladder in the next slim-down unit.

import (
	"fmt"
	"os"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type selfIdentity struct {
	row        *registry.Record
	positional bool
}

func resolveSelfRow(recs []registry.Record, pane herdrcli.Pane) (selfIdentity, string) {
	guid := os.Getenv("HERDER_GUID")
	sessionID := os.Getenv("HCOM_SESSION_ID")

	var guidRow, sessRow *registry.Record
	if guid != "" {
		guidRow = registry.Resolve(recs, guid)
		if guidRow == nil {
			return selfIdentity{}, "HERDER_GUID=" + guid + " has no registry row. If it was inherited from another session's environment, re-run with it unset; to seat this session, run `herder enroll` from this pane."
		}
	}
	if sessionID != "" {
		sessRow = registry.ResolveByToolSessionID(recs, sessionID)
	}
	if guidRow != nil && sessRow != nil && !sameGUID(guidRow, sessRow) {
		return selfIdentity{}, fmt.Sprintf("HERDER_GUID (%s) and HCOM_SESSION_ID (%s) resolve to DIFFERENT identities (%s vs %s) — at least one is stale or inherited. Unset the stale variable and retry, or run `herder enroll` from this pane to re-prove identity.", guid, sessionID, ptrOrEmpty(guidRow.GUID), ptrOrEmpty(sessRow.GUID))
	}
	if row := firstRow(guidRow, sessRow); row != nil {
		return selfIdentity{row: row}, ""
	}

	row := registry.SeatedByPaneOrTerminal(recs, pane.TerminalID)
	if row == nil {
		row = registry.SeatedByPaneOrTerminal(recs, pane.PaneID)
	}
	if row == nil {
		return selfIdentity{positional: true}, "no registry row proves this pane is yours (no HERDER_GUID, no session match, no seated session for terminal " + pane.TerminalID + " or pane " + pane.PaneID + "). Run `herder enroll` from this pane to seat it, then retry."
	}
	wd, _ := os.Getwd()
	paneCWD := pane.ForegroundCWD
	if paneCWD == "" {
		paneCWD = pane.CWD
	}
	if wd == "" || paneCWD == "" || !cwdWithin(wd, paneCWD) {
		return selfIdentity{positional: true}, fmt.Sprintf("positional identity only (terminal %s) and the pane's foreground cwd (%q) does not corroborate this process's cwd (%q). Re-run from that directory (or a subdirectory of it), or run `herder enroll` from this pane to bind a durable identity.", pane.TerminalID, paneCWD, wd)
	}
	return selfIdentity{row: row, positional: true}, ""
}

func cwdWithin(wd, paneCWD string) bool {
	return wd == paneCWD || strings.HasPrefix(wd, strings.TrimRight(paneCWD, "/")+"/")
}

func sameGUID(a, b *registry.Record) bool {
	return a.GUID != nil && b.GUID != nil && *a.GUID == *b.GUID
}

func firstRow(rows ...*registry.Record) *registry.Record {
	for _, row := range rows {
		if row != nil {
			return row
		}
	}
	return nil
}
