package hcomidentity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const supportedSchemaVersion = 17

type LaunchContextRepair struct {
	Status    string
	Code      string
	Cause     string
	Remedy    string
	PaneID    string
	ProcessID string
}

func (r LaunchContextRepair) Refused() bool { return r.Status == "refused" }

// RepairLaunchContext is the sole hcom-database write surface. It validates
// the exact schema contract before beginning a merge-missing-only update, then
// re-reads the row inside the same locked transaction before committing.
func RepairLaunchContext(dir, name, paneID string) LaunchContextRepair {
	refuse := func(code, cause string) LaunchContextRepair {
		return LaunchContextRepair{
			Status: "refused",
			Code:   code,
			Cause:  cause,
			Remedy: launchContextRemedy(code, name),
		}
	}
	if name == "" || paneID == "" {
		return refuse("launch_context_evidence_incomplete", "verified bus name and live pane are both required")
	}
	dbPath, err := hcomDBPath(dir)
	if err != nil {
		return refuse("launch_context_db_unavailable", err.Error())
	}
	if info, err := os.Stat(dbPath); err != nil {
		return refuse("launch_context_db_unavailable", fmt.Sprintf("cannot inspect %s: %v", dbPath, err))
	} else if !info.Mode().IsRegular() {
		return refuse("launch_context_db_unavailable", fmt.Sprintf("%s is not a regular database file", dbPath))
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return refuse("launch_context_db_unavailable", err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return refuse("launch_context_db_unavailable", err.Error())
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return refuse("launch_context_db_unavailable", "cannot configure the hcom database lock timeout: "+err.Error())
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return refuse("launch_context_db_busy", "cannot lock the hcom database for launch-context repair: "+err.Error())
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	if err := validateLaunchContextSchema(ctx, conn); err != nil {
		return refuse("launch_context_schema_mismatch", err.Error())
	}
	var raw string
	if err := conn.QueryRowContext(ctx, "SELECT launch_context FROM instances WHERE name = ?", name).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return refuse("launch_context_row_missing", fmt.Sprintf("joined bus name @%s has no hcom instance row", name))
		}
		return refuse("launch_context_read_failed", err.Error())
	}
	var count int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM instances WHERE name = ?", name).Scan(&count); err != nil || count != 1 {
		if err != nil {
			return refuse("launch_context_read_failed", err.Error())
		}
		return refuse("launch_context_row_ambiguous", fmt.Sprintf("bus name @%s resolves to %d hcom instance rows", name, count))
	}
	fields, err := decodeLaunchContext(raw)
	if err != nil {
		return refuse("launch_context_invalid_json", fmt.Sprintf("@%s launch_context is not a JSON object: %v", name, err))
	}
	existingPane, paneValid := jsonStringField(fields, "pane_id")
	if !paneValid {
		return refuse("launch_context_invalid_coordinate", fmt.Sprintf("@%s launch_context.pane_id is not a string", name))
	}
	if existingPane != "" && existingPane != paneID {
		return refuse("launch_context_pane_conflict", fmt.Sprintf("@%s already records pane_id %q, not verified live pane %q", name, existingPane, paneID))
	}
	existingProcess, processValid := jsonStringField(fields, "process_id")
	if !processValid {
		return refuse("launch_context_invalid_coordinate", fmt.Sprintf("@%s launch_context.process_id is not a string", name))
	}

	changed := false
	if existingPane == "" {
		fields["pane_id"] = jsonStringValue(paneID)
		changed = true
	}
	processID := existingProcess
	if processID == "" {
		processID = uniqueProcessBinding(ctx, conn, name)
		if processID != "" {
			fields["process_id"] = jsonStringValue(processID)
			changed = true
		}
	}
	if !changed {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return refuse("launch_context_commit_failed", err.Error())
		}
		committed = true
		return LaunchContextRepair{Status: "already-present", PaneID: paneID, ProcessID: processID}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return refuse("launch_context_encode_failed", err.Error())
	}
	result, err := conn.ExecContext(ctx, "UPDATE instances SET launch_context = ? WHERE name = ? AND launch_context = ?", string(encoded), name, raw)
	if err != nil {
		return refuse("launch_context_write_failed", err.Error())
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return refuse("launch_context_write_raced", fmt.Sprintf("guarded update affected %d rows", affected))
	}
	var confirmedRaw string
	if err := conn.QueryRowContext(ctx, "SELECT launch_context FROM instances WHERE name = ?", name).Scan(&confirmedRaw); err != nil {
		return refuse("launch_context_confirm_failed", err.Error())
	}
	confirmed, err := decodeLaunchContext(confirmedRaw)
	if err != nil {
		return refuse("launch_context_confirm_failed", "re-read launch_context is not a JSON object")
	}
	confirmedPane, confirmedPaneValid := jsonStringField(confirmed, "pane_id")
	confirmedProcess, confirmedProcessValid := jsonStringField(confirmed, "process_id")
	if !confirmedPaneValid || !confirmedProcessValid || confirmedPane != paneID || (processID != "" && confirmedProcess != processID) {
		return refuse("launch_context_confirm_failed", "re-read did not contain the guarded pane/process merge")
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return refuse("launch_context_commit_failed", err.Error())
	}
	committed = true
	return LaunchContextRepair{Status: "written", PaneID: paneID, ProcessID: processID}
}

type sqlQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func launchContextRemedy(code, name string) string {
	bus := "the intended bus"
	if name != "" {
		bus = "@" + name
	}
	switch code {
	case "launch_context_evidence_incomplete":
		return "Resolve the replacement to a live herdr pane and a verified joined hcom bus before retrying; do not edit the hcom database manually"
	case "launch_context_db_unavailable":
		return "Restore access to the configured hcom data directory and hcom.db, then retry; do not create or edit the database manually"
	case "launch_context_db_busy":
		return "Wait for the active hcom database writer to finish, then retry 'herder reconcile --apply' from the live pane"
	case "launch_context_schema_mismatch":
		return "Restore a compatible hcom data directory, then rerun 'herder reconcile --apply' from a live pane; do not edit the hcom database manually"
	case "launch_context_row_missing":
		return fmt.Sprintf("Join %s to hcom first, then rerun 'herder reconcile --apply' from its live pane", bus)
	case "launch_context_row_ambiguous":
		return fmt.Sprintf("Resolve the duplicate %s instance rows in hcom before retrying; do not choose a row arbitrarily or edit the database manually", bus)
	case "launch_context_pane_conflict":
		return fmt.Sprintf("No herder verb rewrites an existing pane coordinate by design; from the verified live pane, re-create %s through hcom itself so hcom records fresh launch coordinates", bus)
	case "launch_context_invalid_json", "launch_context_invalid_coordinate":
		return fmt.Sprintf("Repair or recreate %s through supported hcom commands, then rerun 'herder reconcile --apply'; do not edit launch_context manually", bus)
	case "launch_context_read_failed":
		return "Check hcom database health and permissions, then retry 'herder reconcile --apply'; no row was changed"
	case "launch_context_encode_failed":
		return "Report the launch-context encoding failure to the herder maintainer; no row was changed"
	case "launch_context_write_failed", "launch_context_write_raced", "launch_context_commit_failed", "launch_context_confirm_failed":
		return "Retry 'herder reconcile --apply' from the verified live pane; if the refusal repeats, check hcom database health and concurrent writers"
	default:
		return "Keep the registry bind unchanged and rerun 'herder reconcile --apply' from a verified live pane; do not edit the hcom database manually"
	}
}

func validateLaunchContextSchema(ctx context.Context, conn *sql.Conn) error {
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("cannot read hcom schema version: %w", err)
	}
	if version != supportedSchemaVersion {
		return fmt.Errorf("unsupported hcom schema version %d (expected %d)", version, supportedSchemaVersion)
	}
	instances, err := tableColumns(ctx, conn, "instances")
	if err != nil {
		return err
	}
	name, ok := instances["name"]
	if !ok || name.pk != 1 || primaryKeyCount(instances) != 1 || !strings.EqualFold(name.kind, "TEXT") {
		return fmt.Errorf("instances.name is not the single TEXT primary key")
	}
	if launch, ok := instances["launch_context"]; !ok || !strings.EqualFold(launch.kind, "TEXT") {
		return fmt.Errorf("instances.launch_context TEXT column is missing")
	}
	bindings, err := tableColumns(ctx, conn, "process_bindings")
	if err != nil {
		return err
	}
	if process, ok := bindings["process_id"]; !ok || process.pk != 1 || primaryKeyCount(bindings) != 1 || !strings.EqualFold(process.kind, "TEXT") {
		return fmt.Errorf("process_bindings.process_id is not the single TEXT primary key")
	}
	if instance, ok := bindings["instance_name"]; !ok || !strings.EqualFold(instance.kind, "TEXT") {
		return fmt.Errorf("process_bindings.instance_name TEXT column is missing")
	}
	if _, ok := bindings["updated_at"]; !ok {
		return fmt.Errorf("process_bindings.updated_at column is missing")
	}
	return nil
}

type columnInfo struct {
	pk   int
	kind string
}

func primaryKeyCount(columns map[string]columnInfo) int {
	count := 0
	for _, column := range columns {
		if column.pk != 0 {
			count++
		}
	}
	return count
}

func tableColumns(ctx context.Context, q sqlQueryer, table string) (map[string]columnInfo, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, fmt.Errorf("cannot inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	columns := map[string]columnInfo{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = columnInfo{pk: pk, kind: kind}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("required hcom table %s is missing", table)
	}
	return columns, nil
}

func decodeLaunchContext(raw string) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if strings.TrimSpace(raw) == "" {
		return fields, nil
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("launch_context is null")
	}
	return fields, nil
}

func jsonStringField(fields map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

func jsonStringValue(value string) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func uniqueProcessBinding(ctx context.Context, conn *sql.Conn, name string) string {
	rows, err := conn.QueryContext(ctx, "SELECT process_id FROM process_bindings WHERE instance_name = ? ORDER BY updated_at DESC", name)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if rows.Scan(&value) == nil && value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 1 {
		return values[0]
	}
	return ""
}

// InstancePID returns the OS process recorded for one exact hcom base name.
// It is a read-only corroboration surface for callers that have already
// selected a live roster row but need to prove which live process owns it.
func InstancePID(dir, baseName string) (int, error) {
	if baseName == "" {
		return 0, fmt.Errorf("hcom base name is required")
	}
	dbPath, err := hcomDBPath(dir)
	if err != nil {
		return 0, err
	}
	if info, statErr := os.Stat(dbPath); statErr != nil {
		return 0, statErr
	} else if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular database file", dbPath)
	}
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var pid sql.NullInt64
	if err := db.QueryRow("SELECT pid FROM instances WHERE name = ?", baseName).Scan(&pid); err != nil {
		return 0, err
	}
	if !pid.Valid || pid.Int64 <= 0 || pid.Int64 > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("hcom instance %q has no live process id", baseName)
	}
	return int(pid.Int64), nil
}

func hcomDBPath(dir string) (string, error) {
	if dir == "" || dir == "null" {
		dir = os.Getenv("HCOM_DIR")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".hcom")
	}
	return filepath.Join(dir, "hcom.db"), nil
}
