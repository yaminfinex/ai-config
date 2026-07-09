package sqlitedsn

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDSNsCarryBusyTimeout(t *testing.T) {
	for name, dsn := range map[string]string{
		"readwrite": ReadWrite("/tmp/store.sqlite"),
		"readonly":  ReadOnly("/tmp/store.sqlite"),
	} {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("%s DSN did not parse: %v", name, err)
		}
		if got := u.Query()["_pragma"]; len(got) != 1 || got[0] != "busy_timeout(5000)" {
			t.Fatalf("%s _pragma = %q, want busy_timeout(5000)", name, got)
		}
	}
	u, err := url.Parse(ReadOnly("/tmp/store.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("mode"); got != "ro" {
		t.Fatalf("readonly mode = %q, want ro", got)
	}
}

func TestRelativePathDSNAcceptedBySQLite(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("relative data", 0o755); err != nil {
		t.Fatal(err)
	}
	dsn := ReadWrite(filepath.Join("relative data", "store ?#%.sqlite"))
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "" {
		t.Fatalf("DSN host = %q, want empty host for file URI", u.Host)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", ReadOnly(filepath.Join("relative data", "store ?#%.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}
