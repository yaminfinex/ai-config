// Package setup installs and reconfigures the per-user shipper service. It
// absorbs etc/install-ship.sh into the binary (doc-002 T1 / design §5): the
// binary path is always the running executable, templates are embedded, the
// store URL lands in the per-OS node-local config with the DP-4b provenance
// rule, and every system interaction happens through a fakeable runner so
// the behavior is unit-testable.
package setup

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed templates/sesh-ship.service
var unitTemplate string

//go:embed templates/dev.sesh.ship.plist.tmpl
var plistTemplate string

// unitPlaceholderExec is the portable placeholder path in the unit template
// that setup replaces with the resolved absolute path of the running binary.
const unitPlaceholderExec = "ExecStart=/usr/local/bin/sesh ship"

const (
	// ServiceName is the systemd user unit name on Linux.
	ServiceName = "sesh-ship.service"
	// LaunchdLabel is the launchd job label on macOS.
	LaunchdLabel = "dev.sesh.ship"
)

// ExecutablePath resolves the running binary, following symlinks so the unit
// pins (and a later `sesh update` replaces) the real file, never a symlink
// into it. A package var so tests can redirect it.
var ExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Runner executes a system command. The default implementation shells out;
// tests substitute a recorder; dry runs print instead of executing.
type Runner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) (string, error)
}

// NewExecRunner returns the Runner that executes commands for real. Shared
// with `sesh update`, whose service restart and verification go through the
// same seam.
func NewExecRunner() Runner { return execRunner{} }

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (execRunner) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// Options configures one setup run. Zero values default to the real host
// environment; tests override the seams.
type Options struct {
	StoreURL string
	Force    bool
	DryRun   bool

	Home   string    // default: os.UserHomeDir()
	OS     string    // "linux" or "darwin"; default: runtime.GOOS
	Exe    string    // default: ExecutablePath()
	UID    int       // default: os.Getuid() (launchctl gui domain)
	User   string    // default: $USER (linger check)
	Runner Runner    // default: real exec
	Out    io.Writer // default: os.Stdout
}

// UnitPath returns the systemd user unit path under home.
func UnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", ServiceName)
}

// DropinPath returns the node-local drop-in path under home.
func DropinPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", ServiceName+".d", "10-local.conf")
}

// PlistPath returns the launchd agent plist path under home.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist")
}

// RenderUnit renders the systemd unit with the pinned absolute binary path.
func RenderUnit(exePath string) string {
	return strings.ReplaceAll(unitTemplate, unitPlaceholderExec, "ExecStart="+exePath+" ship")
}

// Run performs one setup pass: preflight, DP-4b config decision, writes,
// service (re)load. Nothing is written before both the preflight and the
// clobber decision pass, so a refusal leaves the node exactly as found.
func Run(opts Options) error {
	var err error
	if opts.StoreURL == "" {
		return errors.New("sesh setup: --store-url is required")
	}
	if opts.Home == "" {
		if opts.Home, err = os.UserHomeDir(); err != nil {
			return err
		}
	}
	if opts.Exe == "" {
		if opts.Exe, err = ExecutablePath(); err != nil {
			return fmt.Errorf("sesh setup: cannot resolve the running binary: %w", err)
		}
	}
	if !filepath.IsAbs(opts.Exe) {
		return fmt.Errorf("sesh setup: binary path must be absolute (got: %s)", opts.Exe)
	}
	if opts.Runner == nil {
		opts.Runner = execRunner{}
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.User == "" {
		opts.User = os.Getenv("USER")
	}
	switch opts.OS {
	case "linux":
		return runLinux(opts)
	case "darwin":
		return runDarwin(opts)
	default:
		return fmt.Errorf("sesh setup: unsupported platform %s (no Windows in v1)", opts.OS)
	}
}

func (o *Options) say(format string, args ...any) {
	fmt.Fprintf(o.Out, "sesh setup: "+format+"\n", args...)
}

func (o *Options) doit(name string, args ...string) error {
	if o.DryRun {
		fmt.Fprintln(o.Out, "DRY-RUN: "+name+" "+strings.Join(args, " "))
		return nil
	}
	if err := o.Runner.Run(name, args...); err != nil {
		return fmt.Errorf("sesh setup: %s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (o *Options) emit(path string, content []byte) error {
	if o.DryRun {
		fmt.Fprintf(o.Out, "DRY-RUN: would write %s:\n", path)
		for _, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
			fmt.Fprintf(o.Out, "    %s\n", line)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// decide applies the DP-4b decision table to an existing config file.
// A nil error means the write may proceed.
func decide(path string, existing []byte, prov Provenance, force bool) error {
	if existing == nil || force || prov == ProvenanceIntact {
		return nil
	}
	switch prov {
	case ProvenanceEdited:
		return fmt.Errorf("sesh setup: %s has been edited since sesh setup wrote it (provenance digest mismatch) — refusing to overwrite.\n"+
			"  Re-run with --force to replace it (operator env keys other than SESH_STORE_URL are preserved), or edit it directly. Nothing was written.", path)
	default: // ProvenanceLegacy
		return fmt.Errorf("sesh setup: %s exists without a provenance digest (written by hand or by the retired install-ship.sh) — refusing to overwrite.\n"+
			"  Re-run with --force to adopt it under sesh setup. Nothing was written.", path)
	}
}

func runLinux(opts Options) error {
	// Preflight BEFORE any write: a broken user bus (SSH session without
	// lingering, no XDG_RUNTIME_DIR) would otherwise leave a half-installed,
	// never-started service on disk.
	if err := opts.Runner.Run("systemctl", "--user", "show-environment"); err != nil {
		if opts.DryRun {
			opts.say("WARNING: systemd user manager is not reachable on this host; a real")
			opts.say("         run would stop here. Remedy: loginctl enable-linger %s", opts.User)
			opts.say("         (then reconnect so XDG_RUNTIME_DIR is set).")
		} else {
			return fmt.Errorf("sesh setup: cannot talk to the systemd user manager — nothing was written.\n"+
				"  Likely cause: SSH session without lingering (no user bus / XDG_RUNTIME_DIR).\n"+
				"  Remedy: loginctl enable-linger %s   then reconnect and re-run.", opts.User)
		}
	}

	dropin := DropinPath(opts.Home)
	existing, err := os.ReadFile(dropin)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		existing = nil
	}
	if err := decide(dropin, existing, dropinProvenance(existing), opts.Force); err != nil {
		return err
	}

	unitDir := filepath.Dir(UnitPath(opts.Home))
	opts.say("installing systemd user unit into %s", unitDir)
	if err := opts.emit(UnitPath(opts.Home), []byte(RenderUnit(opts.Exe))); err != nil {
		return err
	}
	if err := opts.emit(dropin, RenderDropin(existing, opts.StoreURL)); err != nil {
		return err
	}
	if err := opts.doit("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := opts.doit("systemctl", "--user", "enable", "--now", ServiceName); err != nil {
		return err
	}
	opts.say("installed and started.")
	if linger, err := opts.Runner.Output("loginctl", "show-user", opts.User, "--property=Linger", "--value"); err != nil || linger != "yes" {
		opts.say("NOTE: lingering is not enabled — the unit will NOT survive reboot on a")
		opts.say("      node nobody logs into. Run: loginctl enable-linger %s", opts.User)
	}
	return nil
}

func runDarwin(opts Options) error {
	plist := PlistPath(opts.Home)
	existing, err := os.ReadFile(plist)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		existing = nil
	}
	if err := decide(plist, existing, plistProvenance(existing), opts.Force); err != nil {
		return err
	}
	// A force-adopted foreign plist may be too far from setup's shape to
	// rewrite surgically; fall back to the canonical render.
	content, err := RenderPlist(existing, opts.Exe, opts.StoreURL, opts.Home)
	if err != nil {
		if !opts.Force {
			return err
		}
		if content, err = RenderPlist(nil, opts.Exe, opts.StoreURL, opts.Home); err != nil {
			return err
		}
	}

	opts.say("rendering launchd agent into %s", filepath.Dir(plist))
	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Join(opts.Home, "Library", "Logs"), 0o755); err != nil {
			return err
		}
	}
	if err := opts.emit(plist, content); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", opts.UID)
	// bootout is idempotent cleanup for re-installs; first install has
	// nothing to remove, so its failure is ignored.
	if opts.DryRun {
		fmt.Fprintf(opts.Out, "DRY-RUN: launchctl bootout %s/%s (failure ignored)\n", domain, LaunchdLabel)
	} else {
		_ = opts.Runner.Run("launchctl", "bootout", domain+"/"+LaunchdLabel)
	}
	if err := opts.doit("launchctl", "bootstrap", domain, plist); err != nil {
		return err
	}
	opts.say("installed and bootstrapped (logs: %s).", filepath.Join(opts.Home, "Library", "Logs", "sesh-ship.log"))
	return nil
}
