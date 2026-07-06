package driver

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

	"ai-config/tools/herder/internal/registry"
)

type Hcom struct {
	RegistryPath string
	Bin          string
	Sleep        func(time.Duration)
	Now          func() time.Time
}

type HcomResolution struct {
	Name   string
	Dir    string
	Team   string
	Found  bool
	Refuse bool
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

func (h *Hcom) bin() string {
	if h != nil && h.Bin != "" {
		return h.Bin
	}
	return "hcom"
}

func (h *Hcom) registryPath() string {
	if h != nil && h.RegistryPath != "" {
		return h.RegistryPath
	}
	return registry.DefaultPath()
}

func (h *Hcom) sleep(d time.Duration) {
	if h != nil && h.Sleep != nil {
		h.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (h *Hcom) now() time.Time {
	if h != nil && h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Hcom) Resolve(target string) (HcomResolution, error) {
	rec, found := registryRecordFor(h.registryPath(), target)
	if !found {
		return HcomResolution{Name: target}, nil
	}
	if rec.HcomName != "" && rec.HcomName != "null" {
		return HcomResolution{
			Name:  rec.HcomName,
			Dir:   rec.HcomDir,
			Team:  rec.Team,
			Found: true,
		}, nil
	}
	return HcomResolution{Found: true, Refuse: true}, &ResolveError{Code: 2, Message: fmt.Sprintf("%s has no recorded bus name", target)}
}

func (h *Hcom) Send(target, message string, opts SendOptions, stdout, stderr io.Writer) int {
	if opts.TimeoutMS == 0 {
		opts.TimeoutMS = 3000
	}

	res, err := h.Resolve(target)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) && resolveErr.Code == 2 {
			fmt.Fprintf(stderr, "hcom_send: %s has no recorded bus name (not launched through hcom)\n", target)
			return 2
		}
		return 1
	}

	busName := res.Name
	busDir := res.Dir
	env := os.Environ()
	if busDir != "" && busDir != "null" {
		env = setEnv(env, "HCOM_DIR", busDir)
	}

	if rc := h.runDiscard(env, "list", busName); rc != 0 {
		fmt.Fprintf(stderr, "hcom_send: target %s (@%s) not found on bus (not joined or does not exist)\n", target, busName)
		return 2
	}

	sender := os.Getenv("HERDER_LABEL")
	if sender == "" {
		sender = "orchestrator"
	}
	startISO := h.now().UTC().Format("2006-01-02T15:04:05Z")

	submitted := false
	verifyResult := "not_attempted"
	if rc := h.runDiscard(env, "send", "--from", sender, "@"+busName, "--", message); rc != 0 {
		verifyResult = "not_delivered"
	} else {
		submitted = true
		if h.waitForAck(env, busName, startISO, opts.TimeoutMS) {
			verifyResult = "delivered"
		} else {
			verifyResult = "queued"
		}
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

	if opts.JSONOutput {
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

func (h *Hcom) waitForAck(env []string, busName, startISO string, timeoutMS int) bool {
	windowSeconds := (timeoutMS + 999) / 1000
	start := h.now()
	for {
		if int(h.now().Sub(start).Seconds()) >= windowSeconds {
			return false
		}
		out, rc := h.output(env, "events", "--last", "50", "--context", "deliver:"+busName, "--after", startISO)
		if rc == 0 && jsonArrayLen(out) > 0 {
			return true
		}
		h.sleep(250 * time.Millisecond)
	}
}

func (h *Hcom) runDiscard(env []string, args ...string) int {
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

func (h *Hcom) output(env []string, args ...string) ([]byte, int) {
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

func jsonArrayLen(out []byte) int {
	var arr []json.RawMessage
	if err := json.Unmarshal(out, &arr); err != nil {
		return 0
	}
	return len(arr)
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
