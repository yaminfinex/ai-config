package index

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"sesh/internal/wire"
)

// The append-cost corpus: bulk INSERTs shaped like ingest output (one file
// per logical session, canonical labels already settled) stand in for real
// PUTs purely for build speed — the maintenance queries under measurement
// run against the same tables the ingest path writes.
func buildAppendCostCorpus(tb testing.TB, db *sql.DB, sessions, rowsPerSession int) {
	tb.Helper()
	tx, err := db.Begin()
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	insFile, err := tx.Prepare(`INSERT INTO files
		(tool, session_id, file_uuid, generation, high_water, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tb.Fatal(err)
	}
	insMsg, err := tx.Prepare(`INSERT INTO sesh_index_messages
		(tool, logical_session_id, parsed_logical_session_id, wire_session_id, entry_type,
		 message_uuid, file_uuid, generation, role, timestamp_utc, file_ordinal,
		 line_ordinal, byte_start, byte_end, quarantine, quarantine_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '')`)
	if err != nil {
		tb.Fatal(err)
	}
	insState, err := tx.Prepare(`INSERT INTO index_file_state
		(tool, wire_session_id, file_uuid, generation, complete_offset) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		tb.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("%08d-cccc-4000-8000-000000000000", i)
		at := base.Add(time.Duration(i) * time.Minute)
		var offset int64
		for line := 0; line < rowsPerSession; line++ {
			ts := at.Add(time.Duration(line) * time.Second)
			if _, err := insMsg.Exec(wire.ToolClaude, id, id, id, "message",
				fmt.Sprintf("%08d-1%03d-4000-8000-000000000000", i, line), id, 0, "user",
				ts.Format(time.RFC3339Nano), 0, line, offset, offset+120); err != nil {
				tb.Fatal(err)
			}
			offset += 120
		}
		atStr := at.Format(time.RFC3339Nano)
		if _, err := insFile.Exec(wire.ToolClaude, id, id, 0, offset, atStr, atStr); err != nil {
			tb.Fatal(err)
		}
		if _, err := insState.Exec(wire.ToolClaude, id, id, 0, offset); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
}

// phaseCapture is a slog handler that collects the per-phase durations and
// integer counters the index journals for each append transaction.
type phaseCapture struct {
	mu   sync.Mutex
	sums map[string]time.Duration
	ints map[string]int64
	n    int
}

func (c *phaseCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *phaseCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "index append" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sums == nil {
		c.sums = map[string]time.Duration{}
		c.ints = map[string]int64{}
	}
	c.n++
	r.Attrs(func(a slog.Attr) bool {
		switch v := a.Value.Any().(type) {
		case time.Duration:
			c.sums[a.Key] += v
		case int64:
			c.ints[a.Key] += v
		}
		return true
	})
	return nil
}

func (c *phaseCapture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sums = nil
	c.ints = nil
	c.n = 0
}

func (c *phaseCapture) intSum(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ints[key]
}

func (c *phaseCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *phaseCapture) WithGroup(string) slog.Handler      { return c }

// BenchmarkAppendCostAtCorpusScale is the corpus-scaling measurement behind
// the append-cost work: the same small tail append is timed against corpora
// of 1x/2x/4x sessions, with the per-phase split reported as metrics. A
// bounded append keeps every phase flat across the scales; the pre-fix shape
// grew inherit/unify/dedupe linearly with corpus size.
func BenchmarkAppendCostAtCorpusScale(b *testing.B) {
	const rowsPerSession = 20
	for _, sessions := range []int{2500, 5000, 10000} {
		b.Run(fmt.Sprintf("sessions_%d/tail", sessions), func(b *testing.B) {
			st, idx := newBenchHarness(b)
			defer func() { _ = st.Close() }()
			buildAppendCostCorpus(b, st.DB(), sessions, rowsPerSession)

			// The target is a real resume pair built through ingest, so the
			// measured appends exercise the unified-group maintenance the
			// shippers pay in steady state.
			origSession := syntheticUUID(70_000)
			resumeSession := syntheticUUID(70_001)
			origFile := syntheticUUID(71_000)
			resumeFile := syntheticUUID(71_001)
			orig := syntheticSessionBody(origSession, "cost-orig", 100, time.Date(2026, 7, 9, 13, 0, 0, 0, time.UTC))
			resume := syntheticResumeBody(resumeSession, "cost-resume", []string{"cost-orig-02", "cost-orig-03"}, 100, time.Date(2026, 7, 9, 14, 0, 0, 0, time.UTC))
			putBytesBench(b, st, origSession, origFile, 0, orig)
			if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
				b.Fatal(err)
			}
			putBytesBench(b, st, resumeSession, resumeFile, 0, resume)
			if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
				b.Fatal(err)
			}

			capture := &phaseCapture{}
			prev := slog.Default()
			slog.SetDefault(slog.New(capture))
			defer slog.SetDefault(prev)

			offset := int64(len(resume))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tail := []byte(fmt.Sprintf(
					`{"type":"message","uuid":"cost-tail-%08d","sessionId":"%s","timestamp":"2026-07-09T15:00:00Z","message":{"role":"user"}}`+"\n",
					i, resumeSession))
				putBytesBench(b, st, resumeSession, resumeFile, offset, tail)
				offset += int64(len(tail))
				if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			reportPhases(b, capture)
		})
		b.Run(fmt.Sprintf("sessions_%d/fresh", sessions), func(b *testing.B) {
			st, idx := newBenchHarness(b)
			defer func() { _ = st.Close() }()
			buildAppendCostCorpus(b, st.DB(), sessions, rowsPerSession)

			capture := &phaseCapture{}
			prev := slog.Default()
			slog.SetDefault(slog.New(capture))
			defer slog.SetDefault(prev)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sessionID := syntheticUUID(80_000 + i)
				body := syntheticSessionBody(sessionID, fmt.Sprintf("cost-fresh-%06d", i), 4, time.Date(2026, 7, 9, 16, 0, 0, 0, time.UTC))
				putBytesBench(b, st, sessionID, sessionID, 0, body)
				if err := idx.ProcessAppend(b.Context(), <-st.AppendEvents()); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			reportPhases(b, capture)
		})
	}
}

func reportPhases(b *testing.B, capture *phaseCapture) {
	b.Helper()
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.n == 0 {
		return
	}
	for _, phase := range []string{"parse", "inherit", "insert", "unify", "dedupe", "commit", "total"} {
		b.ReportMetric(float64(capture.sums[phase].Nanoseconds())/float64(capture.n), phase+"-ns/append")
	}
}
