package surface_test

// The fixture-backed fake of the index and mirror (plan U7: the surface
// starts at M0 against fixtures; U6's real index replaces this seam at M2).
// The fake parses the real fixture corpus into frozen-schema rows
// (docs/specs/sesh-wire.md "Message Index Schema"): complete JSONL lines
// only, dedup by (tool, logical_session_id, entry_type, message_uuid),
// file_ordinal by first-ingest order. Logical-session *unification* itself
// (content ids, overlap rule) is U6 logic under U6 tests — specs here state
// each session's membership explicitly, mirroring the frozen rule's outcome
// for the resume pair.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"sesh/internal/surface"
	"sesh/internal/wire"
)

// Fixture file UUIDs, from tests/fixtures/README.md provenance. The content
// sessionId of each claude fixture equals its own file's uuid (verified
// churn property), so the resume pair's logical id is the ORIGINAL file's
// content id — earliest by first-ingest of generation 0 (frozen rule).
const (
	uuidNormal     = "45308169-72e6-4cbe-a05c-2a0025db055e"
	uuidResumeOrig = "2c387aef-72ac-46bc-8ea5-e3b68690a937"
	uuidResumeNew  = "e1be75ad-151b-47fa-9d69-46de1c117843"
	uuidInterleave = "e4578030-c4a9-493f-82e6-de6156d0179a"
	uuidCodexMeta  = "019f01cf-3d22-7ea0-923e-e463b90ea31e"
	uuidGrokChat   = "71ebdd45-2641-49e8-87f5-b8d9f3706714"
	uuidPiBranched = "019f64a0-1111-7222-8333-444444444444"
	// The trailing-partial fixture is a byte prefix of claude-normal; as a
	// distinct session in the fake it needs its own file identity.
	uuidPartial = "0f0f0f0f-1111-2222-3333-444444444444"
)

func fixturesDir() string { return filepath.Join("..", "..", "tests", "fixtures") }

type fixtureFile struct {
	name        string // fixture file name, or "" when bytes is set
	bytes       []byte // literal mirror bytes (synthetic-size tests)
	fileUUID    string
	firstIngest time.Time
}

type sessionSpec struct {
	tool             wire.Tool
	logicalID        string
	hostname, osUser string
	ownerClaims      []string
	tailnetIdentity  string
	mirroredAt       time.Time
	quarantineAll    bool
	quarantineReason string
	files            []fixtureFile
}

type fakeStore struct {
	sessions []surface.SessionSummary
	rows     map[string][]wire.IndexMessage
	mirrors  map[string][]byte
	nodes    []surface.NodeStatus
}

func sessionKey(tool wire.Tool, id string) string { return string(tool) + "/" + id }
func mirrorKey(tool wire.Tool, wireSessionID, fileUUID string, gen int) string {
	return fmt.Sprintf("%s/%s/%s/%d", tool, wireSessionID, fileUUID, gen)
}

// RecentSessions mirrors the seam contract on fixture data: most recent
// first by the R14 instant, logical id tie-break, one page. The fake may
// slice in Go — proving the LIMIT is SQL-side is the live SQLStore's gate.
func (f *fakeStore) RecentSessions(ctx context.Context, limit, offset int) ([]surface.SessionSummary, int, error) {
	return pageSums(f.sessions, limit, offset)
}

// RecentSessionsByNode mirrors the node-filtered contract: the same
// ordering and paging over just the node's sessions.
func (f *fakeStore) RecentSessionsByNode(_ context.Context, hostname, osUser string, limit, offset int) ([]surface.SessionSummary, int, error) {
	var filtered []surface.SessionSummary
	for _, s := range f.sessions {
		if s.Hostname == hostname && s.OSUser == osUser {
			filtered = append(filtered, s)
		}
	}
	return pageSums(filtered, limit, offset)
}

func pageSums(all []surface.SessionSummary, limit, offset int) ([]surface.SessionSummary, int, error) {
	sums := append([]surface.SessionSummary(nil), all...)
	sort.Slice(sums, func(i, j int) bool {
		a, b := sums[i].Recency(), sums[j].Recency()
		if !a.Equal(b) {
			return a.After(b)
		}
		return sums[i].LogicalSessionID < sums[j].LogicalSessionID
	})
	total := len(sums)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(sums) {
		return nil, total, nil
	}
	sums = sums[offset:]
	if limit >= 0 && len(sums) > limit {
		sums = sums[:limit]
	}
	return sums, total, nil
}

func (f *fakeStore) Session(_ context.Context, tool wire.Tool, id string) (surface.SessionSummary, bool, error) {
	for _, s := range f.sessions {
		if s.Tool == tool && s.LogicalSessionID == id {
			return s, true, nil
		}
	}
	return surface.SessionSummary{}, false, nil
}

func (f *fakeStore) Rows(_ context.Context, tool wire.Tool, id string) ([]wire.IndexMessage, error) {
	return append([]wire.IndexMessage(nil), f.rows[sessionKey(tool, id)]...), nil
}

func (f *fakeStore) MirrorRange(_ context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int, start, end int64) ([]byte, error) {
	data, ok := f.mirrors[mirrorKey(tool, wireSessionID, fileUUID, gen)]
	if !ok {
		return nil, fmt.Errorf("no mirror for %s/%s/%s gen %d", tool, wireSessionID, fileUUID, gen)
	}
	if start < 0 || start > int64(len(data)) {
		return nil, fmt.Errorf("range start %d out of mirror size %d", start, len(data))
	}
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return data[start:end], nil
}

func (f *fakeStore) MirrorFile(_ context.Context, tool wire.Tool, wireSessionID, fileUUID string, gen int) (io.ReadCloser, error) {
	data, ok := f.mirrors[mirrorKey(tool, wireSessionID, fileUUID, gen)]
	if !ok {
		return nil, fmt.Errorf("no mirror for %s/%s/%s gen %d", tool, wireSessionID, fileUUID, gen)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeStore) Nodes(context.Context, time.Duration) ([]surface.NodeStatus, error) {
	return append([]surface.NodeStatus(nil), f.nodes...), nil
}

func buildStore(t *testing.T, specs []sessionSpec) *fakeStore {
	t.Helper()
	f := &fakeStore{rows: map[string][]wire.IndexMessage{}, mirrors: map[string][]byte{}}
	for _, spec := range specs {
		f.addSession(t, spec)
	}
	return f
}

func (f *fakeStore) addSession(t *testing.T, spec sessionSpec) {
	t.Helper()
	var (
		rows  []wire.IndexMessage
		maxTS *time.Time
		msgN  int
		quarN int
		files []surface.FileRef
		seen  = map[string]bool{} // dedup key within the logical session
	)
	for ord, file := range spec.files {
		data := file.bytes
		if file.name != "" {
			raw, err := os.ReadFile(filepath.Join(fixturesDir(), file.name))
			if err != nil {
				t.Fatalf("fixture %s: %v", file.name, err)
			}
			data = raw
		}
		// Wire session claim = file uuid, the claude path convention (and
		// what the fake's parseIndexRow sets on every row).
		f.mirrors[mirrorKey(spec.tool, file.fileUUID, file.fileUUID, 0)] = data
		files = append(files, surface.FileRef{WireSessionID: file.fileUUID, FileUUID: file.fileUUID, Generation: 0, FirstIngestAt: file.firstIngest})

		lineOrd := int64(0)
		start := 0
		for i, b := range data {
			if b != '\n' {
				continue
			}
			// Complete lines only; a trailing run without '\n' stays
			// mirrored but out of the index (frozen schema rule).
			line := data[start:i]
			row := parseIndexRow(spec, file.fileUUID, int64(ord), lineOrd, int64(start), int64(i), line)
			start, lineOrd = i+1, lineOrd+1

			if !row.Quarantine && row.MessageUUID != "" {
				key := row.EntryType + "\x00" + row.MessageUUID
				if seen[key] {
					continue // dedup: first occurrence by (file_ordinal, line_ordinal) wins
				}
				seen[key] = true
			}
			if row.Quarantine {
				quarN++
			} else {
				msgN++
				if row.TimestampUTC != nil && (maxTS == nil || row.TimestampUTC.After(*maxTS)) {
					ts := *row.TimestampUTC
					maxTS = &ts
				}
			}
			rows = append(rows, row)
		}
	}
	f.rows[sessionKey(spec.tool, spec.logicalID)] = rows
	f.sessions = append(f.sessions, surface.SessionSummary{
		Tool:             spec.tool,
		LogicalSessionID: spec.logicalID,
		Hostname:         spec.hostname,
		OSUser:           spec.osUser,
		OwnerClaims:      spec.ownerClaims,
		TailnetIdentity:  spec.tailnetIdentity,
		MaxTimestampUTC:  maxTS,
		FirstIngestAt:    spec.files[0].firstIngest,
		MirroredAt:       spec.mirroredAt,
		MessageRows:      msgN,
		QuarantinedRows:  quarN,
		IndexVersion:     int64(len(rows)),
		Files:            files,
	})
}

func parseIndexRow(spec sessionSpec, fileUUID string, fileOrd, lineOrd, byteStart, byteEnd int64, line []byte) wire.IndexMessage {
	row := wire.IndexMessage{
		Tool:             spec.tool,
		LogicalSessionID: spec.logicalID,
		WireSessionID:    fileUUID,
		EntryType:        "unknown",
		FileUUID:         fileUUID,
		Generation:       0,
		Role:             "unknown",
		FileOrdinal:      fileOrd,
		LineOrdinal:      lineOrd,
		ByteStart:        byteStart,
		ByteEnd:          byteEnd,
	}
	if spec.quarantineAll {
		row.Quarantine = true
		row.QuarantineReason = spec.quarantineReason
		return row
	}
	meta := struct {
		Type      string `json:"type"`
		UUID      string `json:"uuid"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Message   *struct {
			Role string `json:"role"`
		} `json:"message"`
		Payload *struct {
			Role string `json:"role"`
		} `json:"payload"`
	}{}
	if err := json.Unmarshal(line, &meta); err != nil {
		row.Quarantine = true
		row.QuarantineReason = "invalid_json"
		return row
	}
	if meta.Type != "" {
		row.EntryType = meta.Type
	}
	if spec.tool == wire.ToolClaude {
		row.MessageUUID = meta.UUID
	}
	if spec.tool == wire.ToolPi && meta.Type != "session" {
		row.MessageUUID = meta.ID
	}
	switch {
	case meta.Message != nil && meta.Message.Role != "":
		row.Role = meta.Message.Role
	case meta.Payload != nil && meta.Payload.Role != "":
		row.Role = meta.Payload.Role
	}
	if spec.tool == wire.ToolGrok {
		// grok lines carry no role field; the indexer derives it from the
		// entry type (U6 rule, mirrored here).
		switch meta.Type {
		case "system", "user", "assistant":
			row.Role = meta.Type
		case "reasoning":
			row.Role = "assistant"
		case "tool_result":
			row.Role = "tool"
		}
	}
	if meta.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, meta.Timestamp); err == nil {
			ts = ts.UTC()
			row.TimestampUTC = &ts
		}
	}
	return row
}

// corpusStore builds the standard five-session store over the full fixture
// corpus, with deterministic ingest instants chosen so that:
//   - claude-normal (last activity 2026-07-02) was ingested BEFORE the
//     resume pair (last activity 2026-06-28, ingested 2026-07-04): parsed
//     recency must sort normal above the pair despite the later ingest.
//   - the trailing-partial session is fully quarantined, so its recency is
//     its first-ingest instant (2026-07-06) and its render is raw-only.
func corpusStore(t *testing.T) *fakeStore {
	t.Helper()
	day := func(d string) time.Time {
		ts, err := time.Parse(time.RFC3339, d)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	f := buildStore(t, []sessionSpec{
		{
			tool: wire.ToolClaude, logicalID: uuidNormal,
			hostname: "workstation", osUser: "grace",
			mirroredAt: day("2026-07-03T10:05:00Z"),
			files:      []fixtureFile{{name: "claude-normal.jsonl", fileUUID: uuidNormal, firstIngest: day("2026-07-03T10:00:00Z")}},
		},
		{
			tool: wire.ToolClaude, logicalID: uuidResumeOrig,
			hostname: "workstation", osUser: "grace",
			mirroredAt: day("2026-07-04T09:02:00Z"),
			files: []fixtureFile{
				{name: "claude-resume-original.jsonl", fileUUID: uuidResumeOrig, firstIngest: day("2026-07-04T09:00:00Z")},
				{name: "claude-resume-new-file.jsonl", fileUUID: uuidResumeNew, firstIngest: day("2026-07-04T09:01:00Z")},
			},
		},
		{
			tool: wire.ToolClaude, logicalID: uuidInterleave,
			hostname: "workstation", osUser: "grace",
			mirroredAt: day("2026-07-05T15:00:00Z"),
			files:      []fixtureFile{{name: "claude-interleaved-writers-standin.jsonl", fileUUID: uuidInterleave, firstIngest: day("2026-07-05T14:55:00Z")}},
		},
		{
			tool: wire.ToolCodex, logicalID: uuidCodexMeta,
			hostname: "laptop", osUser: "alice",
			ownerClaims: []string{"alice"},
			mirroredAt:  day("2026-07-05T08:10:00Z"),
			files:       []fixtureFile{{name: "codex-rollout-meta.jsonl", fileUUID: uuidCodexMeta, firstIngest: day("2026-07-05T08:00:00Z")}},
		},
		{
			// No parsed timestamps anywhere in a grok transcript: recency is
			// the first-ingest instant (frozen fallback), not message time.
			tool: wire.ToolGrok, logicalID: uuidGrokChat,
			hostname: "workstation", osUser: "grace",
			mirroredAt: day("2026-07-05T20:10:00Z"),
			files:      []fixtureFile{{name: "grok-chat-history.jsonl", fileUUID: uuidGrokChat, firstIngest: day("2026-07-05T20:00:00Z")}},
		},
		{
			tool: wire.ToolClaude, logicalID: uuidPartial,
			hostname: "workstation", osUser: "grace",
			mirroredAt:    day("2026-07-06T12:01:00Z"),
			quarantineAll: true, quarantineReason: "parser_rejected",
			files: []fixtureFile{{name: "claude-trailing-partial.jsonl", fileUUID: uuidPartial, firstIngest: day("2026-07-06T12:00:00Z")}},
		},
	})
	// The two fixture nodes, for the '/' entry point (last-seen bookkeeping
	// in the live store).
	// Version census: workstation runs the (pinned, see newServer) current
	// release; laptop lags below the current+previous window and renders
	// the out-of-window flag in the golden.
	f.nodes = []surface.NodeStatus{
		{Hostname: "workstation", OSUser: "grace", LastPutAt: day("2026-07-06T12:01:00Z"), Age: "23h59m0s", Stale: false, ShipperVersion: "sesh-v0.3.2"},
		{Hostname: "laptop", OSUser: "alice", LastPutAt: day("2026-07-05T08:10:00Z"), Age: "51h50m0s", Stale: true, ShipperVersion: "sesh-v0.2.9"},
	}
	return f
}

func piStore(t *testing.T) *fakeStore {
	t.Helper()
	ingest, _ := time.Parse(time.RFC3339, "2026-07-15T12:35:30Z")
	return buildStore(t, []sessionSpec{{
		tool: wire.ToolPi, logicalID: uuidPiBranched,
		hostname: "workstation", osUser: "grace", mirroredAt: ingest.Add(30 * time.Second),
		files: []fixtureFile{{name: "pi-branched-session.jsonl", fileUUID: uuidPiBranched, firstIngest: ingest}},
	}})
}

// inflatedLine re-marshals a real claude-normal user entry with its text
// content inflated to contentSize bytes — real shape, synthetic size, for
// the truncation and display-budget scenarios. Never committed as a fixture
// or golden.
func inflatedLine(t *testing.T, contentSize int) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixturesDir(), "claude-normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	var entry map[string]any
	if err := json.Unmarshal(lines[2], &entry); err != nil { // line 3: plain user text entry
		t.Fatal(err)
	}
	msg, ok := entry["message"].(map[string]any)
	if !ok {
		t.Fatal("claude-normal line 3 has no message object")
	}
	msg["content"] = string(bytes.Repeat([]byte("x"), contentSize))
	giant, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return giant
}

// oneFileStore wraps a single synthetic-size mirror file as one session.
func oneFileStore(t *testing.T, data []byte, quarantineAll bool) *fakeStore {
	t.Helper()
	ingest, _ := time.Parse(time.RFC3339, "2026-07-07T00:00:00Z")
	return buildStore(t, []sessionSpec{{
		tool: wire.ToolClaude, logicalID: uuidNormal,
		hostname: "workstation", osUser: "grace",
		mirroredAt:    ingest,
		quarantineAll: quarantineAll, quarantineReason: "parser_rejected",
		files: []fixtureFile{{bytes: data, fileUUID: uuidNormal, firstIngest: ingest}},
	}})
}

// giantLineStore: one multi-MiB single line (truncation scenario).
func giantLineStore(t *testing.T) *fakeStore {
	t.Helper()
	return oneFileStore(t, append(inflatedLine(t, 3<<20), '\n'), false)
}

// manyLargeLinesStore: n distinct-uuid lines of ~contentSize each — the
// adversarially large mirrored session for the display-budget and
// transcript-window scenarios.
func manyLargeLinesStore(t *testing.T, n, contentSize int, quarantineAll bool) *fakeStore {
	t.Helper()
	line := inflatedLine(t, contentSize)
	var data []byte
	for i := 0; i < n; i++ {
		// Distinct uuids so dedup keeps every line.
		l := bytes.Replace(line, []byte(`"uuid":"`), []byte(fmt.Sprintf(`"uuid":"%08d-`, i)), 1)
		data = append(data, l...)
		data = append(data, '\n')
	}
	return oneFileStore(t, data, quarantineAll)
}

// manyMultiBlockLinesStore: n distinct-uuid lines whose content is `blocks`
// text blocks of blockSize bytes each. One transcript window of such lines
// can exceed the byte budget even though every block obeys the per-block
// cap — the shape the budget backstop exists for.
func manyMultiBlockLinesStore(t *testing.T, n, blocks, blockSize int) *fakeStore {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixturesDir(), "claude-normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	var entry map[string]any
	if err := json.Unmarshal(lines[2], &entry); err != nil { // line 3: plain user text entry
		t.Fatal(err)
	}
	msg, ok := entry["message"].(map[string]any)
	if !ok {
		t.Fatal("claude-normal line 3 has no message object")
	}
	content := make([]map[string]any, blocks)
	for i := range content {
		content[i] = map[string]any{"type": "text", "text": string(bytes.Repeat([]byte("x"), blockSize))}
	}
	msg["content"] = content
	var data []byte
	for i := 0; i < n; i++ {
		entry["uuid"] = fmt.Sprintf("%08d-aaaa-4000-8000-000000000000", i)
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return oneFileStore(t, data, false)
}
