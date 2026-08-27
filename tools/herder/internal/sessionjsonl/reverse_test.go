package sessionjsonl

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanCompleteReverseIgnoresPartialTailAndStops(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "invented.jsonl")
	content := "invented-one\r\ninvented-two\ninvented-partial"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var lines []string
	err := ScanCompleteReverse(path, func(line []byte) bool {
		lines = append(lines, string(line))
		return len(lines) < 2
	})
	if err != nil || !reflect.DeepEqual(lines, []string{"invented-two", "invented-one"}) {
		t.Fatalf("lines = %#v, err = %v", lines, err)
	}
}
