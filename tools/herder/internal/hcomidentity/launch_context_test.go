package hcomidentity

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairLaunchContextWritesMissingCoordinatesAndPreservesFields(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES ('live-self', '{"terminal_preset":"herdr","exact_number":9007199254740993}')`)
	execSQL(t, db, `INSERT INTO process_bindings(process_id, instance_name, updated_at) VALUES ('process-self', 'live-self', 1)`)
	db.Close()

	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "written" || got.PaneID != "pane-self" || got.ProcessID != "process-self" {
		t.Fatalf("repair = %+v, want written pane+process", got)
	}
	fields := readLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self")
	if fields["pane_id"] != "pane-self" || fields["process_id"] != "process-self" || fields["terminal_preset"] != "herdr" {
		t.Fatalf("launch_context = %#v, want preserved preset plus coordinates", fields)
	}
	if raw := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); !strings.Contains(raw, `"exact_number":9007199254740993`) {
		t.Fatalf("launch_context lost exact unknown numeric value: %s", raw)
	}
}

func TestRepairLaunchContextRefusesInvalidExistingCoordinateWithoutWrite(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	raw := `{"pane_id":42,"keep":"yes"}`
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", raw)
	db.Close()

	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "refused" || got.Code != "launch_context_invalid_coordinate" || got.Remedy == "" {
		t.Fatalf("repair = %+v, want typed invalid-coordinate refusal", got)
	}
	if after := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); after != raw {
		t.Fatalf("invalid coordinate refusal changed row: %s", after)
	}
}

func TestRepairLaunchContextAlreadyPresentDoesNotRewrite(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	raw := `{"pane_id":"pane-self","process_id":"process-self","extra":{"keep":true}}`
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", raw)
	db.Close()

	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "already-present" {
		t.Fatalf("repair = %+v, want already-present", got)
	}
	if after := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); after != raw {
		t.Fatalf("already-present row changed:\n before %s\n after  %s", raw, after)
	}
}

func TestRepairLaunchContextRefusesLiveConflictingPaneWithoutWrite(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	raw := `{"pane_id":"pane-foreign","keep":"yes"}`
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", raw)
	db.Close()

	// The recorded pane is still live in herdr — a genuine collision, refused.
	defer withLiveHerdrPanes(t, true, "pane-foreign", "pane-self")()
	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "refused" || got.Code != "launch_context_pane_conflict" || got.Remedy == "" {
		t.Fatalf("repair = %+v, want typed refusal", got)
	}
	if after := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); after != raw {
		t.Fatalf("refused row changed: %s", after)
	}
}

func TestRepairLaunchContextRefusesConflictWhenHerdrUnreadable(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	raw := `{"pane_id":"pane-foreign","keep":"yes"}`
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", raw)
	db.Close()

	// herdr state cannot be read: death is unproven, so stay strict.
	defer withLiveHerdrPanes(t, false)()
	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "refused" || got.Code != "launch_context_pane_conflict" {
		t.Fatalf("repair = %+v, want conflict refusal when herdr is unreadable", got)
	}
	if after := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); after != raw {
		t.Fatalf("unreadable-herdr refusal changed row: %s", after)
	}
}

func TestRepairLaunchContextRepairsStaleDeadPaneAndRebindsProcess(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	// A prior epoch's coordinates: the recorded pane is gone from live herdr,
	// and its process binding no longer exists in process_bindings.
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", `{"pane_id":"pane-dead","process_id":"process-dead","keep":"yes"}`)
	execSQL(t, db, `INSERT INTO process_bindings(process_id, instance_name, updated_at) VALUES ('process-fresh', 'live-self', 2)`)
	db.Close()

	// Only the caller's verified live pane is live; pane-dead is absent.
	defer withLiveHerdrPanes(t, true, "pane-self")()
	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "written" || got.PaneID != "pane-self" || got.ProcessID != "process-fresh" {
		t.Fatalf("repair = %+v, want stale pane rewritten and process rebound", got)
	}
	fields := readLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self")
	if fields["pane_id"] != "pane-self" || fields["process_id"] != "process-fresh" || fields["keep"] != "yes" {
		t.Fatalf("launch_context = %#v, want repaired coordinates with preserved fields", fields)
	}
}

func TestRepairLaunchContextClearsStaleProcessWhenNoLiveBinding(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	// Stale pane and stale process, but no current binding to re-derive: the
	// stale pid must not straddle epochs onto the freshly repaired pane.
	execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES (?, ?)`, "live-self", `{"pane_id":"pane-dead","process_id":"process-dead"}`)
	db.Close()

	defer withLiveHerdrPanes(t, true, "pane-self")()
	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "written" || got.PaneID != "pane-self" || got.ProcessID != "" {
		t.Fatalf("repair = %+v, want stale pane rewritten and stale process cleared", got)
	}
	fields := readLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self")
	if fields["pane_id"] != "pane-self" || fields["process_id"] != "" {
		t.Fatalf("launch_context = %#v, want cleared stale process", fields)
	}
}

func TestRepairLaunchContextSchemaGateRefusesVersionAndInvariantDrift(t *testing.T) {
	tests := []struct {
		name       string
		version    int
		primaryKey bool
	}{
		{name: "version", version: supportedSchemaVersion + 1, primaryKey: true},
		{name: "name not primary key", version: supportedSchemaVersion, primaryKey: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, db := newLaunchContextDB(t, tt.version, tt.primaryKey)
			execSQL(t, db, `INSERT INTO instances(name, launch_context) VALUES ('live-self', '{}')`)
			db.Close()
			got := RepairLaunchContext(dir, "live-self", "pane-self")
			if got.Status != "refused" || got.Code != "launch_context_schema_mismatch" || got.Cause == "" || got.Remedy == "" {
				t.Fatalf("repair = %+v, want typed schema refusal", got)
			}
			if after := readRawLaunchContext(t, filepath.Join(dir, "hcom.db"), "live-self"); after != `{}` {
				t.Fatalf("schema refusal wrote launch context: %s", after)
			}
		})
	}
}

func TestRepairLaunchContextSchemaGateRefusesMissingColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "hcom.db"))
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `CREATE TABLE instances(name TEXT PRIMARY KEY)`)
	execSQL(t, db, `CREATE TABLE process_bindings(process_id TEXT PRIMARY KEY, instance_name TEXT, updated_at REAL NOT NULL)`)
	execSQL(t, db, fmt.Sprintf(`PRAGMA user_version=%d`, supportedSchemaVersion))
	db.Close()

	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "refused" || got.Code != "launch_context_schema_mismatch" || got.Cause != "instances.launch_context TEXT column is missing" || got.Remedy == "" {
		t.Fatalf("repair = %+v, want typed missing-column refusal", got)
	}
}

func TestRepairLaunchContextRefusesMissingDatabaseWithoutCreatingIt(t *testing.T) {
	dir := t.TempDir()
	got := RepairLaunchContext(dir, "live-self", "pane-self")
	if got.Status != "refused" || got.Code != "launch_context_db_unavailable" || got.Remedy == "" {
		t.Fatalf("repair = %+v, want typed unavailable refusal", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "hcom.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing database refusal created a file: %v", err)
	}
}

func TestLaunchContextRefusalRemediesAreCodeSpecific(t *testing.T) {
	tests := []struct {
		code      string
		want      string
		doNotWant string
	}{
		{code: "launch_context_schema_mismatch", want: "compatible hcom data directory"},
		{code: "launch_context_pane_conflict", want: "still live in herdr", doNotWant: "herder reconcile --apply"},
		{code: "launch_context_row_missing", want: "Join @live-self to hcom first", doNotWant: "compatible hcom data directory"},
		{code: "launch_context_row_ambiguous", want: "duplicate @live-self instance rows", doNotWant: "compatible hcom data directory"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := launchContextRemedy(tt.code, "live-self")
			if !strings.Contains(got, tt.want) {
				t.Fatalf("remedy = %q, want substring %q", got, tt.want)
			}
			if tt.doNotWant != "" && strings.Contains(got, tt.doNotWant) {
				t.Fatalf("remedy = %q, do not want substring %q", got, tt.doNotWant)
			}
		})
	}
}

func TestInstancePIDReadsTaggedInstanceByExactBaseNameWithoutCreatingState(t *testing.T) {
	dir, db := newLaunchContextDB(t, supportedSchemaVersion, true)
	execSQL(t, db, `INSERT INTO instances(name, tag, launch_context, pid) VALUES ('zida', 'builder', '{}', 3001551)`)
	db.Close()

	pid, err := InstancePID(dir, "zida")
	if err != nil || pid != 3001551 {
		t.Fatalf("InstancePID(base name) = (%d, %v), want 3001551", pid, err)
	}
	if _, err := InstancePID(dir, "builder-zida"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("InstancePID(full roster name) error = %v, want sql.ErrNoRows", err)
	}
	missingDir := t.TempDir()
	if _, err := InstancePID(missingDir, "zida"); err == nil {
		t.Fatal("InstancePID created or accepted a missing hcom database")
	}
	if _, err := os.Stat(filepath.Join(missingDir, "hcom.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InstancePID missing-db read created state: %v", err)
	}
}

func TestInstancePIDRefusesSchemaDriftLoudly(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, *sql.DB)
		version int
	}{
		{name: "schema version", version: supportedSchemaVersion - 1},
		{
			name:    "pid column",
			version: supportedSchemaVersion,
			setup: func(t *testing.T, db *sql.DB) {
				execSQL(t, db, `ALTER TABLE instances DROP COLUMN pid`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, db := newLaunchContextDB(t, tt.version, true)
			if tt.setup != nil {
				tt.setup(t, db)
			}
			db.Close()

			_, err := InstancePID(dir, "zida")
			if !errors.Is(err, ErrInstancePIDSchemaDrift) || !strings.Contains(err.Error(), "refusing hcom PID corroboration: schema drift:") {
				t.Fatalf("InstancePID schema error = %v, want loud schema-drift refusal", err)
			}
		})
	}
}

// withLiveHerdrPanes swaps the herdr liveness probe for a deterministic set and
// returns a restore func. known=false simulates unreadable herdr state.
func withLiveHerdrPanes(t *testing.T, known bool, panes ...string) func() {
	t.Helper()
	prev := liveHerdrPanes
	liveHerdrPanes = func() (map[string]struct{}, bool) {
		if !known {
			return nil, false
		}
		set := make(map[string]struct{}, len(panes))
		for _, p := range panes {
			set[p] = struct{}{}
		}
		return set, true
	}
	return func() { liveHerdrPanes = prev }
}

func newLaunchContextDB(t *testing.T, version int, primaryKey bool) (string, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "hcom.db"))
	if err != nil {
		t.Fatal(err)
	}
	nameDecl := "TEXT PRIMARY KEY"
	if !primaryKey {
		nameDecl = "TEXT"
	}
	execSQL(t, db, fmt.Sprintf(`CREATE TABLE instances(name %s, tag TEXT, launch_context TEXT DEFAULT '', pid INTEGER DEFAULT NULL)`, nameDecl))
	execSQL(t, db, `CREATE TABLE process_bindings(process_id TEXT PRIMARY KEY, instance_name TEXT, updated_at REAL NOT NULL)`)
	execSQL(t, db, fmt.Sprintf(`PRAGMA user_version=%d`, version))
	return dir, db
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func readRawLaunchContext(t *testing.T, path, name string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow(`SELECT launch_context FROM instances WHERE name = ?`, name).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func readLaunchContext(t *testing.T, path, name string) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(readRawLaunchContext(t, path, name)), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
