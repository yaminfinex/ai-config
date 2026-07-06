package shellquote

import (
	"os/exec"
	"testing"
)

func TestQuoteMatchesRepresentativeBashPrintfQ(t *testing.T) {
	tests := []string{
		"",
		"worker",
		"/tmp/repo/bin/herder",
		"hello world",
		"a$b",
		"a`b",
		"a;b",
		"a|b",
		"a&b",
		"a<b",
		"a>b",
		"a(b)",
		`a"b`,
		"a*b",
		"a'b",
		`back\slash`,
		"line\nbreak",
	}
	for _, input := range tests {
		want := bashPrintfQ(t, input)
		if got := Quote(input); got != want {
			t.Fatalf("Quote(%q) = %q, want bash printf %%q %q", input, got, want)
		}
	}
}

func bashPrintfQ(t *testing.T, input string) string {
	t.Helper()
	out, err := exec.Command("bash", "-c", `printf %q "$1"`, "bash", input).Output()
	if err != nil {
		t.Fatalf("bash printf %%q %q: %v", input, err)
	}
	return string(out)
}
