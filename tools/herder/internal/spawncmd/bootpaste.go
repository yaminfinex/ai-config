package spawncmd

// Keystroke paste engine. Moved verbatim-in-spirit from the retired
// internal/driver keystroke transport (TASK-003): keystroke delivery is not a
// send transport — hcom is THE transport. Two callers remain, neither of them
// a bus-capable initial prompt (those ride hcom since TASK-032: bind-wait,
// then a receipt-verified bus message — this engine's TUI scraping and Enter
// retries manufactured the exact stranded-composer state that starves bus
// delivery): (1) spawn --prompt for BASH agents, which never get a bus
// binding; (2) herder compact, which queues a /compact line into the caller's
// own pane. The TASK-024 evidence gating below (composer-payload checks
// immediately before Enter; cleared-composer degrades to not_delivered, never
// delivered) is a locked floor for both. Package-private by design; `herder
// send` is bus-only.

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"ai-config/tools/herder/internal/herdrcli"
)

type bootPaster struct {
	Client *herdrcli.Client
	Sleep  func(time.Duration)

	// PreflightVisibleOnly restricts the paste preflight to the VISIBLE screen
	// (what is blocking NOW). The boot path keeps the additional scrollback
	// check (default false): a fresh pane's scrollback ≈ its screen, so the
	// stream check costs nothing and catches text mid-redraw. herder compact
	// sets it: a mid-session pane's last 80 scrollback lines legitimately
	// contain answered permission prompts and past interrupts, which are not
	// current blockers.
	PreflightVisibleOnly bool
}

func (b *bootPaster) client() *herdrcli.Client {
	if b != nil && b.Client != nil {
		return b.Client
	}
	return &herdrcli.Client{}
}

func (b *bootPaster) sleep(d time.Duration) {
	if b != nil && b.Sleep != nil {
		b.Sleep(d)
		return
	}
	time.Sleep(d)
}

const bootPasteTimeoutMS = 3000

// paste types message into paneID and verifies delivery, preserving the
// retired transport's contract: ("", 2) preflight refusal (modal/interrupted
// state — nothing typed), ("not_landed", 1) paste never appeared,
// ("not_delivered", 1) placed but submit unconfirmed, ("delivered"|"queued", 0)
// success. The caller maps "" to delivery_result=not_attempted, matching the
// old shell-out where a refusal produced no --json record.
func (b *bootPaster) paste(paneID, message string) (string, int) {
	kind := b.detectKind(paneID)
	sigil := ""
	switch kind {
	case "codex":
		sigil = "›"
	case "claude":
		sigil = "❯"
	}

	preText := stripChrome(b.readPane(paneID))
	preStatus := b.detectStatus(paneID)
	if _, blocked := preflightBlockedReason(preText); blocked && !b.PreflightVisibleOnly {
		return "", 2
	}
	// The scrollback preflight above is blind to alternate-screen overlays:
	// the first-run trust dialog (and claude /login) paint the VISIBLE screen
	// but never enter the recent-unwrapped stream, so a blind paste would land
	// inside the modal. Re-run the block check against the visible source too.
	if _, blocked := preflightBlockedReason(stripChrome(b.readVisible(paneID))); blocked {
		return "", 2
	}

	msgProbe := messageProbe(message)
	preBlobs := pastedBlobCount(preText)
	landed := msgProbe == ""
	// landedConfirmed distinguishes POSITIVE landing evidence (status flip,
	// probe text seen, blob count jump) from the boot-race guard's "assume it
	// landed" fallthrough. It is one of two conditions for counting an empty
	// composer line as submission evidence (the other: the payload still in
	// the composer immediately before the Enter) — on an assumed landing, an
	// empty composer is indistinguishable from a paste that never arrived.
	landedConfirmed := false
	pasteCollapsed := false
	sendAttempts := 0

	for !landed && sendAttempts < 2 {
		if sendAttempts > 0 {
			// Boot-race guard: never blind-paste on top of a buffer we cannot
			// prove is empty. During agent boot the first paste can land while
			// our in-loop detection lags (readPane still returns boot chrome);
			// a second paste then stacks a duplicate, unsubmitted copy. Only
			// re-paste when the composer POSITIVELY shows an empty ready line.
			// If the payload is already there, or the pane is still unreadable,
			// assume the first paste landed and fall through to the submit leg.
			if !composerConfirmedEmpty(stripChrome(b.readPane(paneID)), sigil) {
				landed = true
				break
			}
		}
		sendAttempts++
		if rc, err := b.client().Run("agent", "send", paneID, message); err != nil || rc != 0 {
			b.sleep(400 * time.Millisecond)
			continue
		}
		waited := 0
		for waited < 2500 {
			b.sleep(250 * time.Millisecond)
			if b.statusConfirmsDelivery(paneID, preStatus) {
				landed = true
				landedConfirmed = true
				break
			}
			post := stripChrome(b.readPane(paneID))
			if msgPresent(post, msgProbe) {
				landed = true
				landedConfirmed = true
				break
			}
			if pastedBlobCount(post) > preBlobs {
				landed = true
				landedConfirmed = true
				pasteCollapsed = true
				break
			}
			waited += 250
		}
	}

	if !landed {
		return "not_landed", 1
	}

	submitted := false
	composerEvidence := false
	if msgProbe != "" {
		b.sleep(200 * time.Millisecond)
		// Sample IMMEDIATELY before the Enter: composer-empty may count as
		// submission evidence only if the payload is still in the composer at
		// this instant (codex review P2). A landing seen earlier proves the
		// payload was there at SOME point — an Escape, a modal stealing focus,
		// or a redraw can clear it before the Enter, and an empty composer
		// would then be indistinguishable from a submit. In that case the
		// verify degrades to the other signals (status flip / transcript
		// echo) and reports honestly-unverified rather than delivered.
		preEnter := stripChrome(b.readPane(paneID))
		composerEvidence = landedConfirmed && composerHoldsPayload(preEnter, sigil, msgProbe)
		_, _ = b.client().Run("pane", "send-keys", paneID, "Enter")
		submitted = true
	}

	verifyResult := "not_attempted"
	if submitted {
		verifyResult = "delivered"
		delivered := false
		if pasteCollapsed {
			if b.waitBlobSubmitted(paneID, preBlobs, sigil, composerEvidence) {
				delivered = true
			} else {
				_, _ = b.client().Run("pane", "send-keys", paneID, "Enter")
				if b.waitBlobSubmitted(paneID, preBlobs, sigil, composerEvidence) {
					delivered = true
				} else {
					_, _ = b.client().Run("pane", "send-keys", paneID, "Enter")
					if b.waitBlobSubmitted(paneID, preBlobs, sigil, composerEvidence) {
						delivered = true
					}
				}
			}
		} else if b.pollDelivered(paneID, preStatus, sigil, msgProbe, bootPasteTimeoutMS, composerEvidence) {
			delivered = true
		} else if preStatus == "working" {
			return "queued", 0
		} else if msgTrailingSigil(stripChrome(b.readPane(paneID)), sigil, msgProbe) {
			// This read just saw the payload trailing the composer sigil and
			// the Enter follows immediately, so composer-empty evidence is
			// valid for the re-poll (the same immediately-before-Enter rule).
			_, _ = b.client().Run("pane", "send-keys", paneID, "Enter")
			if b.pollDelivered(paneID, preStatus, sigil, msgProbe, bootPasteTimeoutMS, true) {
				delivered = true
			}
		}
		if !delivered {
			verifyResult = "not_delivered"
		}
	}

	if verifyResult == "not_delivered" {
		return verifyResult, 1
	}
	return verifyResult, 0
}

func (b *bootPaster) readPane(paneID string) string {
	return b.readSource(paneID, "recent-unwrapped")
}

// readVisible reads the pane's VISIBLE screen — the only source that shows an
// alternate-screen overlay (trust dialog, /login), which the recent-unwrapped
// scrollback never captures. Used only by the paste preflight; delivery
// verification and the re-paste guard stay on recent-unwrapped by design.
func (b *bootPaster) readVisible(paneID string) string {
	return b.readSource(paneID, "visible")
}

func (b *bootPaster) readSource(paneID, source string) string {
	out, _, err := b.client().Combined("agent", "read", paneID, "--source", source, "--lines", "80")
	if err != nil {
		return ""
	}
	var envelope struct {
		Result struct {
			Read struct {
				Text string `json:"text"`
			} `json:"read"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return ""
	}
	return envelope.Result.Read.Text
}

func (b *bootPaster) detectKind(paneID string) string {
	for _, agent := range b.agentList() {
		if agent.PaneID == paneID {
			return agent.Agent
		}
	}
	return ""
}

func (b *bootPaster) detectStatus(paneID string) string {
	for _, agent := range b.agentList() {
		if agent.PaneID == paneID {
			return agent.Status
		}
	}
	return ""
}

func (b *bootPaster) agentList() []herdrcli.Agent {
	out, err := b.client().Output("agent", "list")
	if err != nil {
		return nil
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		return nil
	}
	return agents
}

func (b *bootPaster) statusConfirmsDelivery(paneID, preStatus string) bool {
	return b.detectStatus(paneID) == "working" && preStatus != "working"
}

// pollDelivered watches for submission evidence after the Enter: a status
// flip to working, the payload echo leaving the composer for the transcript,
// or — only when the payload was still IN the composer immediately before the
// Enter (composerEvidence) — the composer line coming back positively empty.
// That last signal is what kills the TASK-024 false negatives: a claude
// redraw can drop the echo from the recent-unwrapped window and the herdr
// status flip can lag past the poll window, but a payload that was in the
// composer the instant before exactly one Enter and is gone after it has, by
// elimination, been submitted.
func (b *bootPaster) pollDelivered(paneID, preStatus, sigil, msgProbe string, timeoutMS int, composerEvidence bool) bool {
	elapsed := 0
	for elapsed < timeoutMS {
		b.sleep(250 * time.Millisecond)
		if b.statusConfirmsDelivery(paneID, preStatus) {
			return true
		}
		post := stripChrome(b.readPane(paneID))
		if verifyDelivered(post, sigil, msgProbe) {
			return true
		}
		if composerEvidence && composerLineEmpty(post, sigil) {
			return true
		}
		elapsed += 250
	}
	return false
}

// waitBlobSubmitted's blob-count signal alone is not sufficient: the
// SUBMITTED message echoes into the transcript as another "[Pasted text …]"
// marker, holding the window-wide count up and (pre-TASK-024) driving
// pointless extra Enters into a false not_delivered. So when the blob was
// still in the composer immediately before the Enter (composerEvidence), a
// positively-empty composer line is accepted as submission proof alongside
// the count going back down.
func (b *bootPaster) waitBlobSubmitted(paneID string, preBlobs int, sigil string, composerEvidence bool) bool {
	for i := 0; i < 8; i++ {
		post := stripChrome(b.readPane(paneID))
		if pastedBlobCount(post) <= preBlobs || (composerEvidence && composerLineEmpty(post, sigil)) {
			return true
		}
		b.sleep(300 * time.Millisecond)
	}
	return false
}

var (
	csiRE       = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	oscRE       = regexp.MustCompile(`\x1b\][^\x07]*\x07`)
	pasteBlobRE = regexp.MustCompile(`(?m)\[Pasted (Content|text)`)
)

func stripChrome(text string) string {
	text = csiRE.ReplaceAllString(text, "")
	return oscRE.ReplaceAllString(text, "")
}

func preflightBlockedReason(text string) (string, bool) {
	switch {
	case regexp.MustCompile(`Conversation interrupted|Interrupted by user`).MatchString(text):
		return `agent is in "Conversation interrupted" state`, true
	case trustModalRE.MatchString(text):
		return "first-run directory-trust prompt is open", true
	case regexp.MustCompile(`Sandbox approval|Approve command\?|Allow this command\?`).MatchString(text):
		return "codex approval modal is open", true
	case regexp.MustCompile(`Do you want to allow|Permission required`).MatchString(text):
		return "claude permission prompt is open", true
	default:
		return "", false
	}
}

func pastedBlobCount(text string) int {
	return len(pasteBlobRE.FindAllStringIndex(text, -1))
}

func messageProbe(message string) string {
	lines := strings.Split(message, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 45 {
			line = string(runes[len(runes)-45:])
		}
		return line
	}
	return ""
}

func msgPresent(text, msgProbe string) bool {
	return msgProbe == "" || strings.Contains(text, msgProbe)
}

func msgTrailingSigil(text, sigil, msgProbe string) bool {
	if msgProbe == "" {
		return false
	}
	lines := strings.Split(text, "\n")
	if sigil != "" {
		needle := sigil + " "
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], needle) {
				return strings.Contains(lines[i], msgProbe)
			}
		}
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.Contains(lines[i], msgProbe)
		}
	}
	return false
}

func verifyDelivered(text, sigil, msgProbe string) bool {
	return msgPresent(text, msgProbe) && !msgTrailingSigil(text, sigil, msgProbe)
}

// composerHoldsPayload reports whether the last composer line still shows the
// payload — the literal probe text trailing the sigil, or a collapsed
// "[Pasted …]" blob on the sigil line. This is the immediately-before-Enter
// check that arms composer-empty submission evidence.
func composerHoldsPayload(text, sigil, msgProbe string) bool {
	if msgTrailingSigil(text, sigil, msgProbe) {
		return true
	}
	if sigil == "" {
		return false
	}
	needle := sigil + " "
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.LastIndex(lines[i], needle); idx >= 0 {
			return pasteBlobRE.MatchString(lines[i][idx:])
		}
	}
	return false
}

// composerLineEmpty reports whether the last composer line ("<sigil> …") is
// POSITIVELY present and empty. Unlike composerConfirmedEmpty it looks ONLY at
// the composer line — a "[Pasted text …]" marker elsewhere in the window (the
// transcript echo of a just-submitted paste) does not veto it. Used as
// submission evidence after an Enter; never as a re-paste guard (re-pasting
// stays gated on the stricter composerConfirmedEmpty).
func composerLineEmpty(text, sigil string) bool {
	if sigil == "" {
		return false
	}
	needle := sigil + " "
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.LastIndex(lines[i], needle); idx >= 0 {
			return strings.TrimSpace(lines[i][idx+len(needle):]) == ""
		}
		if strings.TrimRight(lines[i], " ") == sigil {
			return true
		}
	}
	return false
}

// composerConfirmedEmpty reports whether the pane POSITIVELY shows a ready,
// empty input line — the only state in which re-pasting is safe. A booting or
// otherwise unreadable pane (no recognizable sigil, or an empty capture) and a
// pane holding a pasted blob both return false, so the caller never blind-pastes
// a duplicate on top of a first paste it simply cannot see yet.
func composerConfirmedEmpty(text, sigil string) bool {
	return pastedBlobCount(text) == 0 && composerLineEmpty(text, sigil)
}
