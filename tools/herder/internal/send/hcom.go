package send

// The hcom bus delivery engine — moved here from the retired internal/driver
// package (TASK-003). With the herdr keystroke transport removed there is no
// transport abstraction left to select over: send resolves the target to a
// registry row and calls this engine directly.

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf8"

	"ai-config/tools/herder/internal/pendingprompt"
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
// "send_failed" | "sender_unverified" — the caller maps these onto its own
// reporting/exit contract.
func (h *busSender) deliver(sender, busName, busDir, message string, timeoutMS int) string {
	if timeoutMS == 0 {
		timeoutMS = 3000
	}
	if sender == "" {
		return "sender_unverified"
	}

	env := os.Environ()
	if busDir != "" && busDir != "null" {
		env = setEnv(env, "HCOM_DIR", busDir)
	}

	if rc := h.runDiscard(env, "list", busName); rc != 0 {
		return "not_joined"
	}

	// The snapshot-and-send sequence below is only honest when one send at a
	// time runs per (bus, sender, target): two concurrent sends would both
	// snapshot BEFORE either receipt, and the first wake's receipt would
	// satisfy both waiters — the second reporting delivered while actually
	// queued (codex review P2-CONCURRENT). An exact message correlate is not
	// available from hcom 0.7.22 (verified live: the receipt's microsecond
	// msg_ts appears nowhere else — message events expose a second-granular
	// ts and `hcom send` returns no id), so serialize instead: an exclusive
	// inter-process flock spanning snapshot → send → receipt wait. Contention
	// is rare by doctrine (ring once; never blind-resend), so blocking is the
	// simple, shape-proof choice.
	unlock, lockErr := lockSendWindow(busDir, sender, busName)
	if lockErr == nil {
		defer unlock()
	}
	// Backdate the receipt window by one second: --after is second-granular
	// and an instant wake lands the receipt in the SAME second as the send —
	// a strict boundary would exclude it on every poll (seen live, TASK-032).
	// Message-specificity does NOT ride the timestamp: the pre-send snapshot
	// below pins the newest matching receipt's event id, and only a STRICTLY
	// NEWER event counts as THIS send's ack (codex review P2 — without it, a
	// stale receipt from a previous same-sender send inside the window would
	// let this send claim delivered).
	startISO := h.now().UTC().Add(-1 * time.Second).Format("2006-01-02T15:04:05Z")
	receiptArgs := []string{"events", "--last", "50", "--agent", busName, "--context", "deliver:" + sender, "--after", startISO}
	preMax := int64(0)
	if out, rc := h.output(env, receiptArgs...); rc == 0 {
		preMax = maxEventID(out)
	}
	if rc := h.runDiscard(env, "send", "--from", sender, "@"+busName, "--", message); rc != 0 {
		return "send_failed"
	}
	if h.waitForAck(env, receiptArgs, preMax, timeoutMS) {
		return "delivered"
	}
	return "queued"
}

// joined reports whether busName is currently joined on busDir's bus, using
// the same `hcom list <name>` probe deliver() runs before sending (exit 0 ⇒
// joined). It is the tiebreaker disambiguatePane uses to resolve a reused-pane
// coordinate to the one live session (TASK-035); a bus-less row (no recorded
// name) can never be joined and is reported not-live.
func (h *busSender) joined(busName, busDir string) bool {
	if busName == "" || busName == "null" {
		return false
	}
	env := os.Environ()
	if busDir != "" && busDir != "null" {
		env = setEnv(env, "HCOM_DIR", busDir)
	}
	return h.runDiscard(env, "list", busName) == 0
}

// DeliverBus delivers message to a KNOWN bus coordinate (name + bus dir) and
// returns the transport verdict: "delivered" (receipt seen), "queued" (sent,
// no receipt in the window — do NOT resend), "not_joined", "send_failed", or
// "sender_unverified".
// In-process caller: herder spawn's initial-prompt delivery (TASK-032) — spawn
// resolved the coordinate itself from the bind it just observed, so the CLI
// layer's registry resolution would only re-derive the same values.
func DeliverBus(sender, busName, busDir, message string, timeoutMS int) string {
	return (&busSender{}).deliver(sender, busName, busDir, message, timeoutMS)
}

// send delivers message to busName (scoping every hcom call to busDir when the
// registry recorded one) and polls for a delivery receipt. Exit contract is
// unchanged from the driver era: 0 delivered/queued, 1 send failed, 2 target
// not joined on its bus.
func (h *busSender) send(sender, target, busName, busDir, message string, timeoutMS int, jsonOut bool, stdout, stderr io.Writer) int {
	verdict := h.deliver(sender, busName, busDir, message, timeoutMS)
	return h.reportDelivery(verdict, target, busName, busDir, message, jsonOut, stdout, stderr)
}

func (h *busSender) sendPending(registryPath, guid, sender, target, busName, busDir, message string, timeoutMS int, jsonOut bool, stdout, stderr io.Writer) int {
	result, err := pendingprompt.Attempt(registryPath, guid, message, pendingprompt.ActorManual, h.now().UTC(), func(pendingprompt.Record) string {
		return h.deliver(sender, busName, busDir, message, timeoutMS)
	})
	if err != nil {
		fmt.Fprintf(stderr, "hcom_send: pending initial prompt state failed: %v\n", err)
		return 1
	}
	if !result.Managed {
		return h.send(sender, target, busName, busDir, message, timeoutMS, jsonOut, stdout, stderr)
	}
	if result.Suppressed {
		fmt.Fprintf(stderr, "sent 0 chars to %s (hcom @%s), verify=already_delivered (matching pending initial prompt was already submitted; duplicate suppressed)\n", target, busName)
		if jsonOut {
			writeCompactJSON(stdout, hcomRecord{
				Target:         target,
				HcomName:       busName,
				HcomDir:        busDir,
				ResolvedVia:    "registry",
				Submitted:      false,
				Verify:         "already_delivered",
				MessagePreview: messagePreview(message),
			})
		}
		return 0
	}
	return h.reportDelivery(result.Verdict, target, busName, busDir, message, jsonOut, stdout, stderr)
}

func (h *busSender) reportDelivery(verdict, target, busName, busDir, message string, jsonOut bool, stdout, stderr io.Writer) int {
	if verdict == "not_joined" {
		fmt.Fprintf(stderr, "hcom_send: target %s (@%s) not found on bus (not joined or does not exist)\n", target, busName)
		return 2
	}

	submitted := verdict != "send_failed" && verdict != "sender_unverified"
	verifyResult := verdict
	if verdict == "send_failed" || verdict == "sender_unverified" {
		verifyResult = "not_delivered"
	}

	fmt.Fprintf(stderr, "sent %d chars to %s (hcom @%s)", utf8.RuneCountInString(message), target, busName)
	if submitted {
		fmt.Fprint(stderr, ", submitted")
	}
	fmt.Fprintf(stderr, ", verify=%s", verifyResult)
	if verifyResult == "queued" {
		fmt.Fprint(stderr, " (no delivery receipt observed within the verification window; message remains queued — do NOT resend)")
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
// identity the message was sent --from. (The pre-fix query — `--context
// deliver:<target>`, no --agent — matched only receipts of messages the
// TARGET itself had sent, so verify could practically never report
// "delivered".) A receipt counts only if its event id is STRICTLY newer than
// preMax, the newest matching receipt snapshotted before the send — receipts
// carry no message correlate, so the id ordering is what ties the ack to THIS
// send rather than a previous same-sender one (codex review P2).
func (h *busSender) waitForAck(env []string, receiptArgs []string, preMax int64, timeoutMS int) bool {
	windowSeconds := (timeoutMS + 999) / 1000
	start := h.now()
	for {
		if int(h.now().Sub(start).Seconds()) >= windowSeconds {
			return false
		}
		out, rc := h.output(env, receiptArgs...)
		if rc == 0 && maxEventID(out) > preMax {
			return true
		}
		h.sleep(250 * time.Millisecond)
	}
}

// lockSendWindow takes an exclusive inter-process lock over the
// snapshot→send→receipt-wait window for one (bus, sender, target) tuple —
// what makes the strictly-newer-than-snapshot receipt check message-specific
// under concurrency (codex review P2-CONCURRENT). Blocking by design: a
// contending send waits for the holder's verify window rather than racing
// its receipt. The lock file lives in the system temp dir keyed by the tuple
// (not in the bus dir — hcom owns that layout); processes on one machine
// contend, which is the only place the file-DB race exists. On any lock
// error the send proceeds unlocked (best-effort — a delivery must not fail
// on lockfile filesystem trouble).
func lockSendWindow(busDir, sender, busName string) (func(), error) {
	if busDir == "" || busDir == "null" {
		busDir = ambientHcomDir()
	}
	sum := sha256.Sum256([]byte(busDir + "\x00" + sender + "\x00" + busName))
	path := filepath.Join(os.TempDir(), fmt.Sprintf("herder-send-%x.lock", sum[:8]))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return func() {}, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
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

// maxEventID returns the largest event id in `hcom events` output, 0 if none.
// Event ids are the bus's monotone sequence, which is what makes the
// strictly-newer-than-snapshot comparison in waitForAck message-specific.
// Live hcom emits JSONL — one event object per line — NOT a JSON array (seen
// live, TASK-032: an array-only parse saw 0 events on every real receipt, so
// verify could never report delivered even once the query itself was right).
// A JSON array is still accepted for robustness across hcom output modes.
func maxEventID(out []byte) int64 {
	var events []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &events); err != nil {
		for _, line := range bytes.Split(out, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var event struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(line, &event) == nil {
				events = append(events, event)
			}
		}
	}
	max := int64(0)
	for _, event := range events {
		if event.ID > max {
			max = event.ID
		}
	}
	return max
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
