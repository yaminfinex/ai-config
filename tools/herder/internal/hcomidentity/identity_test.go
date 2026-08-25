package hcomidentity

import "testing"

func TestDecodeArrayAndJSONL(t *testing.T) {
	for name, input := range map[string]string{
		"array": `[{"name":"mavu","tool":"codex","status":"active","launch_context":{"pane_id":"p1"}}]`,
		"jsonl": "{\"name\":\"mavu\",\"tool\":\"codex\",\"status\":\"active\",\"launch_context\":{\"pane_id\":\"p1\"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := Decode([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Name != "mavu" || rows[0].LaunchContext.PaneID != "p1" {
				t.Fatalf("rows = %#v", rows)
			}
		})
	}
}

func TestDecodeRejectsMalformedRoster(t *testing.T) {
	if _, err := Decode([]byte(`{"name":`)); err == nil {
		t.Fatal("Decode accepted malformed JSON")
	}
}
