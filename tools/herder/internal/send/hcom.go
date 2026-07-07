package send

// The hcom bus delivery engine — moved here from the retired internal/driver
// package (TASK-003). With the herdr keystroke transport removed there is no
// transport abstraction left to select over: send resolves the target to a
// registry row and calls this engine directly.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
	"unicode/utf8"
)

type busSender struct {
	Bin   string
	Sleep func(time.Duration)
	Now   func() time.Time
}

type hcomRecord struct {
	PaneID         string `json:"pane_id"`
	Agent          string `json:"agent"`
	Target         string `json:"target"`
	HcomName       string `json:"hcom_name"`
	HcomDir        string `json:"hcom_dir"`
	ResolvedVia    string `json:"resolved_via"`
	Submitted      bool   `json:"submitted"`
	Verify         string `json:"verify"`
	MessagePreview string `json:"message_preview"`
}

func (h *busSender) bin() string {
	if h != nil && h.Bin != "" {
		return h.Bin
	}
	return "hcom"
}

func (h *busSender) sleep(d time.Duration) {
	if h != nil && h.Sleep != nil {
		h.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (h *busSender) now() time.Time {
	if h != nil && h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// deliver is the transport core: scope to busDir, confirm the target is joined,
// send, poll for the receipt. Returns "delivered" | "queued" | "not_joined" |
// "send_failed" — the caller maps these onto its own reporting/exit contract.
func (h *busSender) deliver(busName, busDir, message string, timeoutMS int) string {
	if timeoutMS == 0 {
		timeoutMS = 3000
	}

	env := os.Environ()
	if busDir != "" && busDir != "null" {
		env = setEnv(env, "HCOM_DIR", busDir)
	}

	if rc := h.runDiscard(env, "list", busName); rc != 0 {
		return "not_joined"
	}

	sender := senderIdentity()
	// Backdate the receipt window by one second: --after is second-granular
	// and an instant wake lands the receipt in the SAME second as the send —
	// a strict boundary would exclude it on every poll (seen live, TASK-032).
	// The only extra thing a 1s backdate can match is a receipt for a message
	// this same sender sent this same target under a second ago, which the
	// ring-once doctrine forbids anyway.
	startISO := h.now().UTC().Add(-1 * time.Second).Format("2006-01-02T15:04:05Z")
	if rc := h.runDiscard(env, "send", "--from", sender, "@"+busName, "--", message); rc != 0 {
		return "send_failed"
	}
	if h.waitForAck(env, busName, sender, startISO, timeoutMS) {
		return "delivered"
	}
	return "queued"
}

// DeliverBus delivers message to a KNOWN bus coordinate (name + bus dir) and
// returns the transport verdict: "delivered" (receipt seen), "queued" (sent,
// no receipt in the window — do NOT resend), "not_joined", or "send_failed".
// In-process caller: herder spawn's initial-prompt delivery (TASK-032) — spawn
// resolved the coordinate itself from the bind it just observed, so the CLI
// layer's registry resolution would only re-derive the same values.
func DeliverBus(busName, busDir, message string, timeoutMS int) string {
	return (&busSender{}).deliver(busName, busDir, message, timeoutMS)
}

// send delivers message to busName (scoping every hcom call to busDir when the
// registry recorded one) and polls for a delivery receipt. Exit contract is
// unchanged from the driver era: 0 delivered/queued, 1 send failed, 2 target
// not joined on its bus.
func (h *busSender) send(target, busName, busDir, message string, timeoutMS int, jsonOut bool, stdout, stderr io.Writer) int {
	verdict := h.deliver(busName, busDir, message, timeoutMS)
	if verdict == "not_joined" {
		fmt.Fprintf(stderr, "hcom_send: target %s (@%s) not found on bus (not joined or does not exist)\n", target, busName)
		return 2
	}

	submitted := verdict != "send_failed"
	verifyResult := verdict
	if verdict == "send_failed" {
		verifyResult = "not_delivered"
	}

	fmt.Fprintf(stderr, "sent %d chars to %s (hcom @%s)", utf8.RuneCountInString(message), target, busName)
	if submitted {
		fmt.Fprint(stderr, ", submitted")
	}
	fmt.Fprintf(stderr, ", verify=%s", verifyResult)
	if verifyResult == "queued" {
		fmt.Fprint(stderr, " (target was busy; message queued to run next — do NOT resend)")
	}
	fmt.Fprintln(stderr)

	if jsonOut {
		writeCompactJSON(stdout, hcomRecord{
			PaneID:         "",
			Agent:          "agent",
			Target:         target,
			HcomName:       busName,
			HcomDir:        busDir,
			ResolvedVia:    "registry",
			Submitted:      submitted,
			Verify:         verifyResult,
			MessagePreview: messagePreview(message),
		})
	}

	switch verifyResult {
	case "delivered", "queued":
		return 0
	default:
		return 1
	}
}

// waitForAck polls for THIS message's delivery receipt. Receipt shape (pinned
// live, TASK-032): hcom logs delivery on the RECEIVER's instance with context
// `deliver:<SENDER>` — so the query keys on --agent <target> plus the sender
// identity the message was sent --from. The pre-fix query (`--context
// deliver:<target>`, no --agent) matched only receipts of messages the TARGET
// itself had sent, so verify could practically never report "delivered".
func (h *busSender) waitForAck(env []string, busName, sender, startISO string, timeoutMS int) bool {
	windowSeconds := (timeoutMS + 999) / 1000
	start := h.now()
	for {
		if int(h.now().Sub(start).Seconds()) >= windowSeconds {
			return false
		}
		out, rc := h.output(env, "events", "--last", "50", "--agent", busName, "--context", "deliver:"+sender, "--after", startISO)
		if rc == 0 && eventCount(out) > 0 {
			return true
		}
		h.sleep(250 * time.Millisecond)
	}
}

// senderIdentity is the --from identity a send is stamped with — the receipt's
// deliver:<sender> context echoes it verbatim, which is what waitForAck keys on.
func senderIdentity() string {
	sender := os.Getenv("HERDER_LABEL")
	if sender == "" {
		sender = "orchestrator"
	}
	return sender
}

func (h *busSender) runDiscard(env []string, args ...string) int {
	cmd := exec.Command(h.bin(), args...)
	cmd.Env = env
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}

func (h *busSender) output(env []string, args ...string) ([]byte, int) {
	cmd := exec.Command(h.bin(), args...)
	cmd.Env = env
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), exitErr.ExitCode()
	}
	if err != nil {
		return stdout.Bytes(), 1
	}
	return stdout.Bytes(), 0
}

// eventCount counts events in `hcom events` output. Live hcom emits JSONL —
// one event object per line — NOT a JSON array (seen live, TASK-032: the old
// array-only parse returned 0 on every real receipt, so verify could never
// report delivered even once the query itself was right). A JSON array is
// still accepted for robustness across hcom output modes.
func eventCount(out []byte) int {
	var arr []json.RawMessage
	if err := json.Unmarshal(out, &arr); err == nil {
		return len(arr)
	}
	count := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(line, &obj) == nil {
			count++
		}
	}
	return count
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
			cp := append([]string(nil), env...)
			cp[i] = prefix + value
			return cp
		}
	}
	return append(append([]string(nil), env...), prefix+value)
}

func messagePreview(message string) string {
	if len(message) <= 120 {
		return message
	}
	return string([]byte(message)[:120])
}

func writeCompactJSON(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintln(w, string(b))
}
