package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type Herdr struct {
	Client       *herdrcli.Client
	RegistryPath string
	Sleep        func(time.Duration)
}

type Resolution struct {
	PaneID      string
	ResolvedVia string
	Drifted     bool
	DriftNote   string
}

type SendOptions struct {
	NoEnter    bool
	NoVerify   bool
	Force      bool
	TimeoutMS  int
	JSONOutput bool
}

type ResolveError struct {
	Code    int
	Message string
}

func (e *ResolveError) Error() string { return e.Message }

type sendRecord struct {
	PaneID         string `json:"pane_id"`
	Agent          string `json:"agent"`
	Target         string `json:"target"`
	ResolvedVia    string `json:"resolved_via"`
	Submitted      bool   `json:"submitted"`
	Verify         string `json:"verify"`
	ExtraEnterSent bool   `json:"extra_enter_sent"`
	PasteCollapsed bool   `json:"paste_collapsed"`
	MessagePreview string `json:"message_preview"`
}

func (h *Herdr) client() *herdrcli.Client {
	if h != nil && h.Client != nil {
		return h.Client
	}
	return &herdrcli.Client{}
}

func (h *Herdr) registryPath() string {
	if h != nil && h.RegistryPath != "" {
		return h.RegistryPath
	}
	return registry.DefaultPath()
}

func (h *Herdr) sleep(d time.Duration) {
	if h != nil && h.Sleep != nil {
		h.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (h *Herdr) Resolve(target string) (Resolution, error) {
	client := h.client()
	paneOut, _ := client.Output("pane", "list")
	panes, paneParseErr := herdrcli.ParsePaneList(paneOut)

	if strings.HasPrefix(target, "term_") {
		for _, pane := range panes {
			if pane.TerminalID == target {
				return Resolution{PaneID: pane.PaneID, ResolvedVia: "terminal_id(direct)"}, nil
			}
		}
		if paneParseErr != nil || len(panes) == 0 {
			return Resolution{}, &ResolveError{Code: 1, Message: fmt.Sprintf("herdr_resolve: could not read live pane list; not resolving %s", target)}
		}
		return Resolution{}, &ResolveError{Code: 2, Message: fmt.Sprintf("herdr_resolve: terminal %s is not live — agent gone or culled", target)}
	}

	rec, hasRecord, err := h.resolveRegistry(target)
	if err != nil {
		return Resolution{}, err
	}
	if !hasRecord {
		return Resolution{PaneID: target, ResolvedVia: "verbatim"}, nil
	}

	term := rec.TerminalID
	stored := rec.PaneID
	label := stringValue(rec.Label)
	if term == "" {
		return Resolution{PaneID: stored, ResolvedVia: "stored_pane(no terminal_id)"}, nil
	}

	for _, pane := range panes {
		if pane.TerminalID != term {
			continue
		}
		res := Resolution{PaneID: pane.PaneID, ResolvedVia: "terminal_id"}
		if stored != "" && pane.PaneID != stored {
			res.Drifted = true
			name := label
			if name == "" {
				name = target
			}
			res.DriftNote = fmt.Sprintf("pane drifted — %s spawned at %s, terminal %s now at %s", name, stored, term, pane.PaneID)
		}
		return res, nil
	}

	if paneParseErr != nil || len(panes) == 0 {
		return Resolution{}, &ResolveError{Code: 1, Message: fmt.Sprintf("herdr_resolve: could not read live pane list; not resolving %s", target)}
	}
	name := label
	if name == "" {
		name = target
	}
	return Resolution{}, &ResolveError{Code: 2, Message: fmt.Sprintf("herdr_resolve: %s (terminal %s) is not live — agent gone or culled", name, term)}
}

func (h *Herdr) resolveRegistry(target string) (registry.Record, bool, error) {
	recs, err := registry.Load(h.registryPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return registry.Record{}, false, nil
		}
		return registry.Record{}, false, err
	}
	rec := registry.Resolve(recs, target)
	if rec == nil {
		return registry.Record{}, false, nil
	}
	return *rec, true, nil
}

func (h *Herdr) Send(target, message string, opts SendOptions, stdout, stderr io.Writer) int {
	if opts.TimeoutMS == 0 {
		opts.TimeoutMS = 3000
	}

	res, err := h.Resolve(target)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			fmt.Fprintln(stderr, resolveErr.Message)
			if resolveErr.Code == 1 {
				return 1
			}
			return 2
		}
		return 1
	}
	if res.DriftNote != "" {
		fmt.Fprintf(stderr, "herder-send: %s\n", res.DriftNote)
	}

	paneID := res.PaneID
	kind := h.detectKind(paneID)
	sigil := ""
	switch kind {
	case "codex":
		sigil = "›"
	case "claude":
		sigil = "❯"
	}

	preText := stripChrome(h.readPane(paneID))
	preStatus := h.detectStatus(paneID)
	if !opts.Force {
		if reason, blocked := preflightBlockedReason(preText); blocked {
			fmt.Fprintf(stderr, "herder-send: refusing to send to %s: %s\n", paneID, reason)
			return 2
		}
	}

	msgProbe := messageProbe(message)
	preBlobs := pastedBlobCount(preText)
	landed := msgProbe == ""
	pasteCollapsed := false
	sendAttempts := 0

	for !landed && sendAttempts < 2 {
		sendAttempts++
		if rc, err := h.client().Run("agent", "send", paneID, message); err != nil || rc != 0 {
			h.sleep(400 * time.Millisecond)
			continue
		}
		waited := 0
		for waited < 2500 {
			h.sleep(250 * time.Millisecond)
			if h.statusConfirmsDelivery(paneID, preStatus) {
				landed = true
				break
			}
			post := stripChrome(h.readPane(paneID))
			if msgPresent(post, msgProbe) {
				landed = true
				break
			}
			if pastedBlobCount(post) > preBlobs {
				landed = true
				pasteCollapsed = true
				break
			}
			waited += 250
		}
	}

	if !landed {
		fmt.Fprintf(stderr, "herder-send: message never landed in %s after %d paste attempts (agent not accepting input?)\n", paneID, sendAttempts)
	}

	submitted := false
	if !opts.NoEnter && landed && msgProbe != "" {
		h.sleep(200 * time.Millisecond)
		_, _ = h.client().Run("pane", "send-keys", paneID, "Enter")
		submitted = true
	}

	verifyResult := "not_attempted"
	extraEnterSent := false
	if !landed {
		verifyResult = "not_landed"
	} else if opts.NoEnter {
		verifyResult = "placed"
	} else if opts.NoVerify {
		verifyResult = "not_verified"
	} else if submitted {
		delivered := false
		queued := false
		if pasteCollapsed {
			if h.waitBlobSubmitted(paneID, preBlobs) {
				delivered = true
			} else {
				_, _ = h.client().Run("pane", "send-keys", paneID, "Enter")
				extraEnterSent = true
				if h.waitBlobSubmitted(paneID, preBlobs) {
					delivered = true
				} else {
					_, _ = h.client().Run("pane", "send-keys", paneID, "Enter")
					if h.waitBlobSubmitted(paneID, preBlobs) {
						delivered = true
					}
				}
			}
		} else if h.pollDelivered(paneID, preStatus, sigil, msgProbe, opts.TimeoutMS) {
			delivered = true
		} else if preStatus == "working" {
			queued = true
			delivered = true
		} else if msgTrailingSigil(stripChrome(h.readPane(paneID)), sigil, msgProbe) {
			_, _ = h.client().Run("pane", "send-keys", paneID, "Enter")
			extraEnterSent = true
			if h.pollDelivered(paneID, preStatus, sigil, msgProbe, opts.TimeoutMS) {
				delivered = true
			}
		}

		if queued {
			verifyResult = "queued"
		} else if delivered {
			verifyResult = "delivered"
		} else {
			verifyResult = "not_delivered"
		}
	}

	fmt.Fprintf(stderr, "sent %d chars to %s", utf8.RuneCountInString(message), paneID)
	if kind != "" {
		fmt.Fprintf(stderr, " (%s)", kind)
	}
	if submitted {
		fmt.Fprint(stderr, ", submitted")
	} else if opts.NoEnter {
		fmt.Fprint(stderr, ", not submitted (--no-enter)")
	}
	fmt.Fprintf(stderr, ", verify=%s", verifyResult)
	if verifyResult == "queued" {
		fmt.Fprint(stderr, " (target was busy; message queued to run next — do NOT resend)")
	}
	if pasteCollapsed {
		fmt.Fprint(stderr, " (codex collapsed paste to blob)")
	}
	if extraEnterSent {
		fmt.Fprint(stderr, " (extra Enter sent)")
	}
	if sendAttempts > 1 {
		fmt.Fprintf(stderr, " (re-pasted x%d)", sendAttempts)
	}
	fmt.Fprintln(stderr)

	if opts.JSONOutput {
		record := sendRecord{
			PaneID:         paneID,
			Agent:          kind,
			Target:         target,
			ResolvedVia:    res.ResolvedVia,
			Submitted:      submitted,
			Verify:         verifyResult,
			ExtraEnterSent: extraEnterSent,
			PasteCollapsed: pasteCollapsed,
			MessagePreview: messagePreview(message),
		}
		writeCompactJSON(stdout, record)
	}

	switch verifyResult {
	case "not_landed", "not_delivered":
		return 1
	default:
		return 0
	}
}

func (h *Herdr) readPane(paneID string) string {
	out, _, err := h.client().Combined("agent", "read", paneID, "--source", "recent-unwrapped", "--lines", "80")
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

func (h *Herdr) detectKind(paneID string) string {
	agents := h.agentList()
	for _, agent := range agents {
		if agent.PaneID == paneID {
			return agent.Agent
		}
	}
	return ""
}

func (h *Herdr) detectStatus(paneID string) string {
	agents := h.agentList()
	for _, agent := range agents {
		if agent.PaneID == paneID {
			return agent.Status
		}
	}
	return ""
}

func (h *Herdr) agentList() []herdrcli.Agent {
	out, err := h.client().Output("agent", "list")
	if err != nil {
		return nil
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		return nil
	}
	return agents
}

func (h *Herdr) statusConfirmsDelivery(paneID, preStatus string) bool {
	return h.detectStatus(paneID) == "working" && preStatus != "working"
}

func (h *Herdr) pollDelivered(paneID, preStatus, sigil, msgProbe string, timeoutMS int) bool {
	elapsed := 0
	for elapsed < timeoutMS {
		h.sleep(250 * time.Millisecond)
		if h.statusConfirmsDelivery(paneID, preStatus) {
			return true
		}
		post := stripChrome(h.readPane(paneID))
		if verifyDelivered(post, sigil, msgProbe) {
			return true
		}
		elapsed += 250
	}
	return false
}

func (h *Herdr) waitBlobSubmitted(paneID string, preBlobs int) bool {
	for i := 0; i < 8; i++ {
		if pastedBlobCount(stripChrome(h.readPane(paneID))) <= preBlobs {
			return true
		}
		h.sleep(300 * time.Millisecond)
	}
	return false
}

var (
	csiRE        = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	oscRE        = regexp.MustCompile(`\x1b\][^\x07]*\x07`)
	pasteBlobRE  = regexp.MustCompile(`(?m)\[Pasted (Content|text)`)
	trustModalRE = regexp.MustCompile(`Do you trust the contents of this directory|Do you trust the files in this folder|Is this a project you created or one you trust|Yes, I trust this folder`)
)

func stripChrome(text string) string {
	text = csiRE.ReplaceAllString(text, "")
	return oscRE.ReplaceAllString(text, "")
}

func preflightBlockedReason(text string) (string, bool) {
	switch {
	case regexp.MustCompile(`Conversation interrupted|Interrupted by user`).MatchString(text):
		return `agent is in "Conversation interrupted" state; recover it first (focus pane and press Enter, or --force)`, true
	case trustModalRE.MatchString(text):
		return "first-run directory-trust prompt is open; accept it (focus pane + Enter) before sending", true
	case regexp.MustCompile(`Sandbox approval|Approve command\?|Allow this command\?`).MatchString(text):
		return "codex approval modal is open; resolve it manually before sending", true
	case regexp.MustCompile(`Do you want to allow|Permission required`).MatchString(text):
		return "claude permission prompt is open; resolve it manually before sending", true
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

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
