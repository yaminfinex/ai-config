package sqlitedsn

import (
	"net/url"
	"testing"
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
