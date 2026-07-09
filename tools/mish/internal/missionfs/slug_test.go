package missionfs

import "testing"

func TestValidateSlugRefusesACTableWithOneLineReason(t *testing.T) {
	for _, slug := range []string{"Perf_Regression", "-x", "a--b", "x-", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		err := ValidateSlug(slug)
		if err == nil {
			t.Fatalf("ValidateSlug(%q) succeeded, want refusal", slug)
		}
		if got := err.Error(); got == "" || containsNewline(got) {
			t.Fatalf("ValidateSlug(%q) reason = %q, want one line", slug, got)
		}
	}
}

func TestValidateSlugAcceptsSpecShape(t *testing.T) {
	for _, slug := range []string{"a", "x1", "perf-regression", "abc123", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("ValidateSlug(%q) = %v, want nil", slug, err)
		}
	}
}

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}
