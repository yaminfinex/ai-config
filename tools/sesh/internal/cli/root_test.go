package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpListsAllSubcommands(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help: unexpected error: %v", err)
	}
	for _, name := range []string{"ship", "serve", "reindex", "status", "admin"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("--help output missing subcommand %q\n%s", name, out.String())
		}
	}
}

// TestShipRequiresStoreURL: `ship` is real since U4; its whole config
// surface is the store URL, and running without one must fail up front
// rather than start a daemon that can only hold position.
func TestShipRequiresStoreURL(t *testing.T) {
	t.Setenv("SESH_STORE_URL", "")
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ship"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "SESH_STORE_URL") {
		t.Errorf("sesh ship without a store URL: want config error naming SESH_STORE_URL, got %v", err)
	}
}

func TestStubsReportNotImplemented(t *testing.T) {
	stubs := [][]string{
		{"serve"},
		{"reindex"},
		{"status"},
		{"admin", "drop-file"},
	}
	for _, args := range stubs {
		root := newRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Errorf("sesh %s: expected not-implemented error, got nil", strings.Join(args, " "))
			continue
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Errorf("sesh %s: error %q does not say not implemented", strings.Join(args, " "), err)
		}
	}
}

func TestBareAdminErrors(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"admin"})
	if err := root.Execute(); err == nil {
		t.Error("sesh admin without a subcommand should error")
	}
}
