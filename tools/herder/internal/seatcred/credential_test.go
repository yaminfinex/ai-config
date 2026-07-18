package seatcred

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestEnvironmentAloneSelectsNothing(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-parent")
	t.Setenv("HCOM_SESSION_ID", "session-parent")
	t.Setenv("HERDR_PANE_ID", "pane-parent")
	_, err := Authenticate(filepath.Join(t.TempDir(), "registry.jsonl"), "")
	if !errors.Is(err, ErrCredentialRequired) {
		t.Fatalf("Authenticate env-only error = %v", err)
	}
}

func TestRotationProtocolAndAcceptedSameUIDRead(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "state", "registry.jsonl")
	guid := "guid-seat"
	old, err := Stage(registryPath, guid)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Dir(filepath.Dir(old.Path)):                  0o700,
		filepath.Dir(old.Path):                                0o700,
		filepath.Join(filepath.Dir(old.Path), ".rotate.lock"): 0o600,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != want {
			t.Fatalf("credential state %s mode=%v err=%v, want %v", path, info, statErr, want)
		}
	}
	setGeneration(t, registryPath, guid, old.File.Generation)
	if err := old.Close(registryPath, old.File.Generation); err != nil {
		t.Fatal(err)
	}
	oldPath := old.Path

	selected, err := Authenticate(registryPath, oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if selected.GUID != guid || selected.AuditRef != guid+"/"+old.File.Generation {
		t.Fatalf("selection = %+v", selected)
	}

	// A deliberate same-uid copy remains accepted by the stated trust boundary.
	copyPath := filepath.Join(t.TempDir(), "deliberate-copy.token")
	data, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if copied, err := Authenticate(registryPath, copyPath); err != nil || copied.AuditRef != selected.AuditRef {
		t.Fatalf("deliberate same-uid read selection=%+v err=%v", copied, err)
	}
	audit, err := os.ReadFile(filepath.Join(filepath.Dir(registryPath), "credential-audit.jsonl"))
	if err != nil || !strings.Contains(string(audit), `"presentation":"same-uid-copy"`) || strings.Contains(string(audit), old.File.Token) {
		t.Fatalf("credential audit=%q err=%v", audit, err)
	}

	// Crash before/during staging: a partial non-current file never authenticates;
	// the registry-current old generation remains exactly the one working token.
	partialPath := CredentialPath(registryPath, guid, "partial")
	if err := os.WriteFile(partialPath, []byte(`{"version":1`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertExactlyWorking(t, registryPath, oldPath, partialPath)

	// Crash after staging and before the flip has the same safety shape.
	next, err := Stage(registryPath, guid)
	if err != nil {
		t.Fatal(err)
	}
	assertExactlyWorking(t, registryPath, oldPath, next.Path)

	// The locked registry append is the sole commit point.
	setGeneration(t, registryPath, guid, next.File.Generation)
	assertExactlyWorking(t, registryPath, next.Path, oldPath)
	if err := next.Close(registryPath, next.File.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("just-retired generation was not retained as dead state: %v", err)
	}
	if _, err := Authenticate(registryPath, oldPath); !errors.Is(err, ErrStaleCredential) {
		t.Fatalf("retained-dead generation authentication error = %v, want stale refusal", err)
	}
	unknownPath := filepath.Join(filepath.Dir(oldPath), "operator-note")
	if err := os.WriteFile(unknownPath, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(filepath.Dir(oldPath), "foreign.token")
	if err := os.Symlink(oldPath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	// Only a later completion collects files that were already non-current when
	// it acquired the per-guid lock. The registry-current token remains live,
	// and cleanup never follows or removes unknown/symlink entries.
	later, err := Stage(registryPath, guid)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Abort()
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dead generation survived a later completion's lazy GC: %v", err)
	}
	for _, path := range []string{unknownPath, symlinkPath} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("lazy GC removed protected entry %s: %v", path, err)
		}
	}
	assertExactlyWorking(t, registryPath, next.Path, later.Path)

	registryBytes, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registryBytes), next.File.Token) || strings.Contains(string(registryBytes), old.File.Token) {
		t.Fatal("registry contains credential token material")
	}
	info, err := os.Stat(next.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
}

func TestCredentialSelectionPrecedesPoisonedAmbientCorrelates(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	staged, err := Stage(registryPath, "guid-selected")
	if err != nil {
		t.Fatal(err)
	}
	setGeneration(t, registryPath, "guid-selected", staged.File.Generation)
	if err := staged.Close(registryPath, staged.File.Generation); err != nil {
		t.Fatal(err)
	}
	selected, err := Authenticate(registryPath, staged.Path)
	if err != nil {
		t.Fatal(err)
	}
	joined := true
	rows := []hcomidentity.Row{
		{Name: "bus-selected", Joined: &joined, SessionID: "sid-selected", LaunchContext: hcomidentity.LaunchContext{ProcessID: "pid-selected", PaneID: "pane-selected"}},
		{Name: "bus-poison", Joined: &joined, SessionID: "sid-poison", LaunchContext: hcomidentity.LaunchContext{ProcessID: "pid-poison", PaneID: "pane-poison"}},
	}
	for name, evidence := range map[string]hcomidentity.Evidence{
		"name":       {Name: "bus-poison"},
		"session_id": {SessionID: "sid-poison"},
		"process_id": {ProcessID: "pid-poison"},
		"pane_id":    {PaneIDs: []string{"pane-poison"}},
	} {
		if err := VerifySelectedBus(rows, selected, evidence); err == nil || !strings.Contains(err.Error(), "without re-selection") {
			t.Fatalf("poisoned %s correlate error = %v", name, err)
		}
	}
	if err := VerifySelectedBus(rows, selected, hcomidentity.Evidence{}); err != nil {
		t.Fatalf("scrubbed ambient verification = %v", err)
	}
}

func TestCredentialPathAnchorsToRegistryNotHOMEOrWorktree(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "isolated-state", "registry.jsonl")
	t.Setenv("HOME", filepath.Join(t.TempDir(), "different-home"))
	path := CredentialPath(registryPath, "guid", "generation")
	want := filepath.Join(filepath.Dir(registryPath), "credentials", "guid", "generation.token")
	if path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
}

func setGeneration(t *testing.T, registryPath, guid, generation string) {
	t.Helper()
	outcomes, err := registry.UpdateLocked(registryPath, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		row := registry.V2ByGUID(tx.Projection, guid)
		if row == nil {
			row = &v2.SessionRecord{Kind: v2.KindSession, GUID: guid, Event: "seated", State: v2.StateSeated, Label: guid, Tool: "codex"}
		} else {
			copy := *row
			copy.Raw = nil
			row = &copy
		}
		row.Event = "seated"
		row.RecordedAt = ""
		row.State = v2.StateSeated
		row.Seat = &v2.Seat{Kind: "herdr", PaneID: "pane-" + guid, TerminalID: "terminal-" + guid, HcomName: "bus-selected", CredentialGeneration: generation}
		return []v2.SessionRecord{*row}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func assertExactlyWorking(t *testing.T, registryPath, working string, refused ...string) {
	t.Helper()
	if _, err := Authenticate(registryPath, working); err != nil {
		t.Fatalf("working credential %s refused: %v", working, err)
	}
	workingCount := 1
	for _, path := range refused {
		if _, err := Authenticate(registryPath, path); err == nil {
			workingCount++
			t.Errorf("non-current credential %s authenticated", path)
		}
	}
	if workingCount != 1 {
		t.Fatalf("working generations = %d", workingCount)
	}
}

func TestCredentialPayloadIsVersionedAndBound(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	staged, err := Stage(registryPath, "guid-bound")
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Abort()
	data, err := os.ReadFile(staged.Path)
	if err != nil {
		t.Fatal(err)
	}
	var payload File
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != Version || payload.GUID != "guid-bound" || payload.Generation != staged.File.Generation || payload.Token == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestStageRefusesSymlinkedCredentialRoot(t *testing.T) {
	state := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(state, "credentials")); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(filepath.Join(state, "registry.jsonl"), "guid-seat"); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("Stage symlink error = %v", err)
	}
}
