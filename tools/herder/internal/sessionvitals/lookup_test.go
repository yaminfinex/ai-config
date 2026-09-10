package sessionvitals

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdersock"
)

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// Reddens: cache bypassed (hit case has NO transcript, so a fallback returns
// empty vitals); any socket failure made fatal or slow.
func TestReadPrefersCacheThenDirect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeID := "73100000-0000-4000-8000-000000000731"
	row := hcomidentity.Row{Name: "impl-vamo", Tool: "claude", Directory: "/invented/violet", SessionID: claudeID}
	path := filepath.Join(home, ".claude", "projects", "-invented-violet", claudeID+".jsonl")
	copyFixture(t, filepath.Join("..", "claudesession", "testdata", "vitals.jsonl"), path, time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	direct, err := ReadDirect(row)
	if err != nil || direct.Vitals.Model == "" {
		t.Fatalf("direct = %+v, %v", direct, err)
	}
	cached := claudesession.Vitals{Model: "invented-from-cache", ContextUsage: &claudesession.ContextUsage{UsedTokens: 4242, InputTokens: 4242}}
	stamp := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string) func()
		source  string
		model   string
		wantAt  time.Time
		wantMax time.Duration
	}{
		{"absent", func(*testing.T, string) func() { return func() {} }, "direct", direct.Vitals.Model, direct.ObservedAt, 50 * time.Millisecond},
		{"stale", func(t *testing.T, dir string) func() {
			l, err := net.Listen("unix", herdersock.Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			l.(*net.UnixListener).SetUnlinkOnClose(false)
			_ = l.Close()
			return func() {}
		}, "direct", direct.Vitals.Model, direct.ObservedAt, 50 * time.Millisecond},
		{"hung", func(t *testing.T, dir string) func() {
			l, err := net.Listen("unix", herdersock.Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					defer c.Close()
				}
			}()
			return func() { _ = l.Close() }
		}, "direct", direct.Vitals.Model, direct.ObservedAt, herdersock.ClientBudget * 3},
		{"miss", func(t *testing.T, dir string) func() {
			s, err := herdersock.Listen(dir, func(herdersock.Request) herdersock.Response { return herdersock.Response{Miss: true} }, nil)
			if err != nil {
				t.Fatal(err)
			}
			return s.Close
		}, "direct", direct.Vitals.Model, direct.ObservedAt, 50 * time.Millisecond},
		{"hit", func(t *testing.T, dir string) func() {
			s, err := herdersock.Listen(dir, func(r herdersock.Request) herdersock.Response {
				if r.Op != herdersock.OpVitals || r.Tool != "claude" || r.Session != claudeID {
					t.Errorf("request = %+v", r)
				}
				return herdersock.Response{Vitals: cached, Path: path, Phase: "tailing", ObservedAt: stamp}
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return s.Close
		}, "cache", "invented-from-cache", stamp, 50 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := shortDir(t)
			t.Setenv("HERDER_STATE_DIR", dir)
			stop := tc.setup(t, dir)
			defer stop()
			start := time.Now()
			got, err := Read(row)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			if got.Source != tc.source || got.Vitals.Model != tc.model || !got.ObservedAt.Equal(tc.wantAt) {
				t.Fatalf("Read = source %q model %q at %s; want %q %q %s", got.Source, got.Vitals.Model, got.ObservedAt, tc.source, tc.model, tc.wantAt)
			}
			if elapsed > tc.wantMax {
				t.Fatalf("%s: %s exceeds %s", tc.name, elapsed, tc.wantMax)
			}
			if tc.name == "stale" {
				if _, err := os.Stat(herdersock.Path(dir)); !os.IsNotExist(err) {
					t.Fatal("stale socket not unlinked")
				}
			}
		})
	}
}

// Reddens: Seed/Advance drifting from the reverse reader (shared-reader
// invariant at the primitive level).
func TestSeedAdvanceMatchReadDirect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexID := "73200000-0000-4000-8000-000000000732"
	path := filepath.Join(home, ".codex", "sessions", "2026", "09", "10", "rollout-invented-"+codexID+".jsonl")
	copyFixture(t, filepath.Join("..", "codexsession", "testdata", "vitals.jsonl"), path, time.Now())
	row := hcomidentity.Row{Tool: "codex", SessionID: codexID}
	vitals, offset, err := Seed("codex", false, path)
	if err != nil {
		t.Fatal(err)
	}
	direct, _ := ReadDirect(row)
	if !reflect.DeepEqual(vitals, direct.Vitals) {
		t.Fatalf("seed %+v != direct %+v", vitals, direct.Vitals)
	}
	extra := `{"type":"turn_context","payload":{"model":"invented-codex-later"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":2020,"cached_input_tokens":1,"output_tokens":2},"model_context_window":258400}}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":3030` // partial line held back
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString(extra)
	_ = f.Close()
	advanced, next, read, err := Advance("codex", false, path, offset, vitals)
	if err != nil {
		t.Fatal(err)
	}
	direct, _ = ReadDirect(row)
	if !reflect.DeepEqual(advanced, direct.Vitals) || advanced.Model != "invented-codex-later" || advanced.ContextUsage.UsedTokens != 2020 {
		t.Fatalf("advance %+v != direct %+v", advanced, direct.Vitals)
	}
	if next-offset != read || read == 0 || next == offset+int64(len(extra)) {
		t.Fatalf("offset %d→%d read %d (partial tail must be held back)", offset, next, read)
	}
	if err := os.Truncate(path, offset-1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Advance("codex", false, path, next, advanced); err != ErrTruncated {
		t.Fatalf("truncation err = %v", err)
	}
}
