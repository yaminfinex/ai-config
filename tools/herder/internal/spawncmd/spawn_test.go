package spawncmd

import (
	"encoding/json"
	"testing"
)

func TestHcomEntryAcceptsNumericCreatedAt(t *testing.T) {
	var entries []hcomEntry
	data := []byte(`[{"name":"smoke-p5-tuna","tag":"smoke-p5","directory":"/tmp","created_at":1782979094.0}]`)
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := string(entries[0].CreatedAt); got != "1782979094.0" {
		t.Fatalf("CreatedAt = %q, want numeric value preserved", got)
	}
}
