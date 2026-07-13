//go:build !linux

package launchcmd

import "syscall"

func grokBridgeProcessAttributes(bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func grokBridgeHardKillFenced() bool { return false }
