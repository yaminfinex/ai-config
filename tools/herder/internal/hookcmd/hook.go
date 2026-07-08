// Package hookcmd implements `herder hook <verb> [args...]`, the wrapper that
// herder-spawned agents run in place of raw hcom. herder spawn prepends the
// shim dir to the child's PATH, so the installed `hcom` PATH shim (and, through
// it, the Claude hooks' `${HCOM:-hcom} <verb>` calls AND the agent's own hcom
// CLI usage) all land here.
//
// The env-var vector (HCOM="herder hook") is dead: hcom unconditionally
// re-exports HCOM=hcom to its own launch children, clobbering any interception.
// The PATH shim survives that because it does not depend on an env var hcom
// controls.
//
// For every verb EXCEPT sessionstart the wrapper is invisible: it execs the
// real hcom with byte-for-byte stdin/stdout/stderr/exit passthrough — including
// interactive/pty stdin, since syscall.Exec replaces the process image and
// preserves the fd table. It degrades to a silent exit 0 when no real hcom can
// be found, mirroring the hook string's own `command -v … || exit 0`. For
// sessionstart it runs real hcom (preserving its DB/registration side-effects
// and name assignment), then REWRITES the injected bootstrap's
// additionalContext to herder-native doctrine before emitting.
//
// RECURSION TRAP: the hcom shim now sits on PATH as `hcom`, so a naive
// LookPath("hcom") would resolve back to the shim → `herder hook` → shim → …
// forever. Two guards: (1) the shim exports HERDER_HOOK_HCOM to the real
// binary's absolute path before exec'ing us, which we prefer; (2) our PATH-walk
// fallback explicitly skips this herder's shim dir. Neither guard rides an env
// var that survives into the launched agent, so the sessionstart rewrite still
// fires for every fresh boot.
package hookcmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"ai-config/tools/herder/internal/herderpaths"
)

// resolveRealHcom finds the genuine hcom binary while never resolving back to
// the hcom PATH shim (which would recurse into `herder hook`). It prefers the
// HERDER_HOOK_HCOM absolute path the shim exports; failing that it walks PATH
// itself, skipping this herder's shim dir. Returns "" when no real hcom exists.
func resolveRealHcom() string {
	if v := os.Getenv("HERDER_HOOK_HCOM"); v != "" && isExecutableFile(v) {
		return v
	}
	skip := shimsDir()
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if skip != "" && sameDir(dir, skip) {
			continue
		}
		cand := filepath.Join(dir, "hcom")
		if isExecutableFile(cand) {
			return cand
		}
	}
	return ""
}

// shimsDir best-effort resolves this herder's shim directory so the PATH walk
// can skip it. An empty return just means "don't skip anything" — safe, because
// the shim's HERDER_HOOK_HCOM export is the primary guard.
func shimsDir() string {
	p, err := herderpaths.Resolve()
	if err != nil {
		return ""
	}
	return p.ShimsDir
}

func sameDir(a, b string) bool {
	ca, cb := canonicalDir(a), canonicalDir(b)
	return ca != "" && ca == cb
}

func canonicalDir(d string) string {
	abs, err := filepath.Abs(d)
	if err != nil {
		return ""
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		return ev
	}
	return abs
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// Run dispatches one hcom invocation routed through the shim. args[0] is the
// verb/subcommand (e.g. "pre", "post", "sessionstart", "send"); the remainder
// are forwarded verbatim. Everything but sessionstart is a transparent hcom
// passthrough, so the agent's own hcom CLI keeps working byte-for-byte.
func Run(args []string, stdout, stderr io.Writer) int {
	hcomPath := resolveRealHcom()
	if hcomPath == "" {
		// No real hcom → silent exit 0, mirroring the hook string's own
		// `command -v … || exit 0`. Never break session boot over a missing bus.
		return 0
	}

	if len(args) > 0 && args[0] == "sessionstart" {
		return runSessionStart(hcomPath, args, stdout, stderr)
	}
	return passthrough(hcomPath, args)
}

// passthrough replaces this process with real hcom, so stdin/stdout/stderr and
// the exit code are the genuine article — the wrapper leaves no trace, and
// interactive/pty stdin flows straight through the preserved fd table.
func passthrough(hcomPath string, args []string) int {
	argv := append([]string{"hcom"}, args...)
	if err := syscall.Exec(hcomPath, argv, os.Environ()); err != nil {
		// Exec only returns on failure; fall back to a subprocess so a weird
		// exec error still runs the verb rather than dropping it.
		cmd := exec.Command(hcomPath, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if runErr := cmd.Run(); runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				return exitErr.ExitCode()
			}
			return 1
		}
	}
	return 0
}

// runSessionStart runs real hcom sessionstart, captures its emitted JSON, and
// rewrites the injected additionalContext to herder doctrine. Any failure to
// parse/extract degrades to hcom's ORIGINAL output unmodified — a rewrite must
// never break session boot.
func runSessionStart(hcomPath string, args []string, stdout, stderr io.Writer) int {
	var out bytes.Buffer
	cmd := exec.Command(hcomPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = &out
	cmd.Stderr = stderr
	runErr := cmd.Run()
	rc := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else {
			rc = 1
		}
	}

	original := out.Bytes()
	rewritten, ok := rewriteSessionStart(original)
	if !ok || rc != 0 {
		// Degrade safe: emit exactly what hcom produced.
		_, _ = stdout.Write(original)
		return rc
	}
	_, _ = stdout.Write(rewritten)
	return rc
}

// rewriteSessionStart parses hcom's sessionstart JSON, extracts the dynamic
// values from the stable lines of its additionalContext, and swaps in the
// herder-native bootstrap. Returns (rewritten, true) on success, or (_, false)
// when the payload can't be parsed or a required value is missing.
func rewriteSessionStart(original []byte) ([]byte, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(original, &root); err != nil {
		return nil, false
	}
	hsoRaw, ok := root["hookSpecificOutput"]
	if !ok {
		return nil, false
	}
	var hso map[string]json.RawMessage
	if err := json.Unmarshal(hsoRaw, &hso); err != nil {
		return nil, false
	}
	acRaw, ok := hso["additionalContext"]
	if !ok {
		return nil, false
	}
	var ac string
	if err := json.Unmarshal(acRaw, &ac); err != nil {
		return nil, false
	}

	vals, ok := extract(ac)
	if !ok {
		return nil, false
	}

	newAC := renderBootstrap(vals)
	newACRaw, err := json.Marshal(newAC)
	if err != nil {
		return nil, false
	}
	hso["additionalContext"] = newACRaw
	newHSORaw, err := json.Marshal(hso)
	if err != nil {
		return nil, false
	}
	root["hookSpecificOutput"] = newHSORaw
	rewritten, err := json.Marshal(root)
	if err != nil {
		return nil, false
	}
	// Preserve hcom's trailing newline if it had one, so downstream byte-diffs
	// of the envelope stay minimal.
	if bytes.HasSuffix(original, []byte("\n")) {
		rewritten = append(rewritten, '\n')
	}
	return rewritten, true
}

type bootstrapVals struct {
	displayName     string
	instanceName    string
	sender          string
	tag             string
	activeInstances string
}

var (
	reName   = regexp.MustCompile(`(?m)^\s*-?\s*Your name:\s*(.+?)\s*$`)
	reMarker = regexp.MustCompile(`\[hcom:([^\]]+)\]`)
	reSender = regexp.MustCompile(`Prioritize @(\S+)`)
	// hcom quotes the tag with double quotes through 0.7.22 and single quotes
	// from 0.7.23 on. Accept either, matched-pair only (no mixed "foo' ), and
	// carry the value in whichever alternative fired — extraction is identical
	// for both stock bootstraps, so the rendered output is byte-stable.
	reTag    = regexp.MustCompile(`You are tagged (?:"([^"]+)"|'([^']+)')`)
	reActive = regexp.MustCompile(`(?m)^(Active \(snapshot\):.*)$`)
)

// extract pulls the dynamic values off hcom's rendered bootstrap. displayName,
// instanceName, and sender are required — if any is missing the caller degrades
// to the original text. tag and activeInstances are optional (their lines are
// conditional).
func extract(ac string) (bootstrapVals, bool) {
	var v bootstrapVals
	if m := reName.FindStringSubmatch(ac); m != nil {
		v.displayName = strings.TrimSpace(m[1])
	}
	if m := reMarker.FindStringSubmatch(ac); m != nil {
		v.instanceName = strings.TrimSpace(m[1])
	}
	if m := reSender.FindStringSubmatch(ac); m != nil {
		v.sender = strings.TrimSpace(m[1])
	}
	if m := reTag.FindStringSubmatch(ac); m != nil {
		// m[1] is the double-quoted capture, m[2] the single-quoted one; exactly
		// one is non-empty for a matched pair.
		tag := m[1]
		if tag == "" {
			tag = m[2]
		}
		v.tag = strings.TrimSpace(tag)
	}
	if m := reActive.FindStringSubmatch(ac); m != nil {
		v.activeInstances = strings.TrimSpace(m[1])
	}
	if v.displayName == "" || v.instanceName == "" || v.sender == "" {
		return v, false
	}
	return v, true
}

const tagLine = "You are tagged '{tag}'. Message your group: hcom send @{tag}- -- msg"

// renderBootstrap fills the baked template. The active-instances and tag lines
// are dropped wholesale when their value is empty (per the drafter's flag: the
// {tag} group line renders ONLY when the tag is non-empty).
func renderBootstrap(v bootstrapVals) string {
	t := bootstrapTemplate
	t = strings.ReplaceAll(t, "{display_name}", v.displayName)
	t = strings.ReplaceAll(t, "{instance_name}", v.instanceName)
	t = strings.ReplaceAll(t, "{SENDER}", v.sender)

	if v.activeInstances == "" {
		t = strings.ReplaceAll(t, "{active_instances}\n", "")
	} else {
		t = strings.ReplaceAll(t, "{active_instances}", v.activeInstances)
	}

	if v.tag == "" {
		t = strings.ReplaceAll(t, "\n\n"+tagLine, "")
	} else {
		t = strings.ReplaceAll(t, "{tag}", v.tag)
	}
	return t
}
