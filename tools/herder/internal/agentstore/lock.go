package agentstore

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// lockFile takes flock(LOCK_EX) on file, polling LOCK_NB every pollInterval
// until timeout. flock has no native deadline, and a register call must never
// block on anything but this lock, bounded.
func lockFile(file *os.File, timeout time.Duration) error {
	const pollInterval = 5 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return fmt.Errorf("flock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock timeout after %s (another writer holds events.jsonl)", timeout)
		}
		time.Sleep(pollInterval)
	}
}

func unlockFile(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
