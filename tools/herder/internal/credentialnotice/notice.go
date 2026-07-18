// Package credentialnotice durably suppresses blind retries of the one
// non-secret credential-path notice sent to a newly completed seat.
package credentialnotice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Record struct {
	GUID       string `json:"guid"`
	Generation string `json:"generation"`
	Path       string `json:"path"`
	Sender     string `json:"sender"`
	Recipient  string `json:"recipient"`
	BusDir     string `json:"bus_dir"`
	Verdict    string `json:"verdict"`
	Attempted  string `json:"attempted_at"`
}

type Result struct {
	Verdict    string
	Suppressed bool
}

// Attempt records intent before delivery. An interrupted "attempting" record
// is terminal: automatic recovery never blind-resends an outcome it cannot
// distinguish from a queued delivery.
func Attempt(registryPath string, record Record, deliver func(string, string, string, string, string, int) string) (Result, error) {
	if record.GUID == "" || record.Generation == "" || record.Path == "" || record.Sender == "" || record.Recipient == "" || strings.ContainsAny(record.GUID+record.Generation, `/\\`) {
		return Result{}, fmt.Errorf("credential notice requires exact guid, generation, path, sender, and recipient")
	}
	dir := filepath.Join(filepath.Dir(registryPath), "credential-notices", record.GUID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Result{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, record.Generation+".json")
	if data, readErr := os.ReadFile(path); readErr == nil {
		var prior Record
		if json.Unmarshal(data, &prior) == nil && prior.Generation == record.Generation {
			return Result{Verdict: prior.Verdict, Suppressed: true}, nil
		}
		return Result{Verdict: "unknown", Suppressed: true}, fmt.Errorf("credential notice receipt is unreadable; blind resend suppressed")
	} else if !os.IsNotExist(readErr) {
		return Result{}, readErr
	}
	record.Verdict = "attempting"
	record.Attempted = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeAtomic(path, record); err != nil {
		return Result{}, err
	}
	message := fmt.Sprintf("Seat credential ready. generation=%s path=%s. Pass this file as `--credential-file PATH`; the token is never carried in environment variables or messages.", record.Generation, record.Path)
	thread := "credential:" + record.GUID + ":" + record.Generation
	verdict := deliver(record.Sender, record.Recipient, record.BusDir, thread, message, 3000)
	record.Verdict = verdict
	if err := writeAtomic(path, record); err != nil {
		return Result{Verdict: verdict}, err
	}
	return Result{Verdict: verdict}, nil
}

func writeAtomic(path string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notice-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
