//go:build linux

package launchcmd

import "syscall"

func grokBridgeProcessAttributes(manual bool) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{Setsid: true}
	if manual {
		// The foreground wrapper normally sends a generation-fenced retire.
		// Pdeathsig closes the uncatchable SIGKILL gap for a manual guest: the
		// supervisor receives SIGTERM and its retire-on-stop policy converges the
		// journal after the binder exits.
		attr.Pdeathsig = syscall.SIGTERM
	}
	return attr
}

func grokBridgeHardKillFenced() bool { return true }
