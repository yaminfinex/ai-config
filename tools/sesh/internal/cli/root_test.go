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

func TestStubsReportNotImplemented(t *testing.T) {
	stubs := [][]string{
		{"ship"},
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

func TestServeRejectsNonLoopbackBind(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"serve", "--addr", "0.0.0.0:0", "--data-dir", t.TempDir()})
	err := root.Execute()
	if err == nil {
		t.Fatal("sesh serve should reject non-loopback bind before M4")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("serve error %q does not mention loopback", err)
	}
}

func TestReindexRunsOnEmptyStore(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"reindex", "--data-dir", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatalf("sesh reindex on empty store: %v", err)
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
