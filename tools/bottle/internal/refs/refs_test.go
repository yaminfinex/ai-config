package refs

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		in      string
		name    string
		version int
	}{
		{"auth-expert", "auth-expert", 0},
		{"auth-expert@2", "auth-expert", 2},
		{"a", "a", 0},
		{"9lives", "9lives", 0},
		{"a-b-c@13", "a-b-c", 13},
	}
	for _, c := range cases {
		ref, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", c.in, err)
			continue
		}
		if ref.Name != c.name || ref.Version != c.version {
			t.Errorf("Parse(%q) = {%q, %d}, want {%q, %d}", c.in, ref.Name, ref.Version, c.name, c.version)
		}
		if got, want := ref.Pinned(), c.version > 0; got != want {
			t.Errorf("Parse(%q).Pinned() = %v, want %v", c.in, got, want)
		}
	}
}

func TestParseInvalidNames(t *testing.T) {
	// Invalid names must be rejected with the regex in the error message.
	for _, in := range []string{"Foo", "-x", "", "a_b", "a b", "@2", "Foo@1"} {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q): expected error, got nil", in)
			continue
		}
		if !strings.Contains(err.Error(), NamePattern) {
			t.Errorf("Parse(%q) error %q does not contain the name regex %q", in, err, NamePattern)
		}
	}
}

func TestParseInvalidVersions(t *testing.T) {
	for _, in := range []string{"a@b", "a@", "a@0", "a@-1", "a@1.5", "a@1@2"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", in)
		}
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName("auth-expert"); err != nil {
		t.Errorf("ValidateName(valid): %v", err)
	}
	// @ is reserved for version refs: a bare *name* containing @ is invalid.
	for _, in := range []string{"a@b", "a@2", "Foo", "-x", ""} {
		err := ValidateName(in)
		if err == nil {
			t.Errorf("ValidateName(%q): expected error, got nil", in)
			continue
		}
		if !strings.Contains(err.Error(), NamePattern) {
			t.Errorf("ValidateName(%q) error %q does not contain the name regex %q", in, err, NamePattern)
		}
	}
}

func TestString(t *testing.T) {
	if got := (Ref{Name: "x", Version: 0}).String(); got != "x" {
		t.Errorf("unpinned String() = %q, want %q", got, "x")
	}
	if got := (Ref{Name: "x", Version: 3}).String(); got != "x@3" {
		t.Errorf("pinned String() = %q, want %q", got, "x@3")
	}
}
