package grokbridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// RetireOffline marks every unacked receipt undeliverable only after acquiring
// the seat's exclusive binder lock. Acquiring that lock is the proof that no
// live binder can be writing the journal beside this recovery path.
func RetireOffline(stateDir, seat string) (int, error) {
	dir := SeatDir(stateDir, seat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("prepare Grok seat recovery directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "bridge.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open Grok seat recovery lock: %w", err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return 0, errors.New("seat binder lock is still held while its socket is unavailable; the binder is alive but not serving — inspect the seat bridge log, wait for the supervisor to restore the socket, then retry the cull")
		}
		return 0, fmt.Errorf("acquire Grok seat recovery lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	journalPath := filepath.Join(dir, "journal.jsonl")
	if _, err = os.Stat(journalPath); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("inspect Grok seat journal before offline retirement: %w", err)
	}
	journal, err := OpenJournal(journalPath)
	if err != nil {
		return 0, fmt.Errorf("open Grok seat journal for offline retirement: %w", err)
	}
	defer journal.Close()
	_, err = journal.RetireUnacked(journal.Generation())
	if err != nil {
		return 0, fmt.Errorf("retire Grok seat journal offline: %w", err)
	}
	_, retired := journal.Counts()
	return retired, nil
}
