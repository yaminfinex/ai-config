package credentialnotice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttemptSendsPathAndGenerationWithoutTokenAndSuppressesRetry(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	calls := 0
	message := ""
	record := Record{
		GUID: "guid-child", Generation: "generation-one", Path: "/state/credentials/guid-child/generation-one.token",
		Sender: "worker-parent", Recipient: "worker-child", BusDir: "/bus",
	}
	deliver := func(sender, recipient, busDir, thread, body string, verifyMS int) string {
		calls++
		message = body
		if sender != record.Sender || recipient != record.Recipient || busDir != record.BusDir || verifyMS != 3000 {
			t.Fatalf("delivery coordinates = %q %q %q %d", sender, recipient, busDir, verifyMS)
		}
		if thread != "credential:"+record.GUID+":"+record.Generation {
			t.Fatalf("thread=%q", thread)
		}
		return "queued"
	}
	first, err := Attempt(registryPath, record, deliver)
	if err != nil || first.Verdict != "queued" || first.Suppressed {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	second, err := Attempt(registryPath, record, deliver)
	if err != nil || second.Verdict != "queued" || !second.Suppressed || calls != 1 {
		t.Fatalf("second = %+v calls=%d err=%v", second, calls, err)
	}
	for _, want := range []string{record.Generation, record.Path, "--credential-file PATH"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q missing %q", message, want)
		}
	}
	if strings.Contains(message, "token-material") {
		t.Fatalf("message leaked token material: %q", message)
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(registryPath), "credential-notices", record.GUID, record.Generation+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("notice mode=%v err=%v", info, err)
	}
}
