// Package seatcred owns minted per-seat credentials and their rotation protocol.
package seatcred

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"golang.org/x/sys/unix"
)

const (
	Version       = 1
	credentialDir = "credentials"
	cutoverFile   = "cutover-v1"
	maxFileBytes  = 4096
)

// CutoverEnabled reports whether the owner has completed the literal-100%
// issuance gate. Before this marker exists the old binary-compatible identity
// path remains active so rollout can issue credentials without a flag day.
func CutoverEnabled(registryPath string) bool {
	file, err := openOwnedRegular(filepath.Join(filepath.Dir(registryPath), credentialDir, cutoverFile), unix.O_RDONLY, 0, false)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && info.Mode().Perm()&0o077 == 0
}

// EnableCutover durably commits the credential-authenticated verb cutover.
func EnableCutover(registryPath string) error {
	stateDir := filepath.Dir(registryPath)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	root := filepath.Join(stateDir, credentialDir)
	if err := ensureOwnedDir(root); err != nil {
		return err
	}
	path := filepath.Join(root, cutoverFile)
	file, err := openOwnedRegular(path, unix.O_CREAT|unix.O_WRONLY, 0o600, true)
	if err != nil {
		return err
	}
	if err = file.Truncate(0); err == nil {
		_, err = file.Write([]byte("credential-cutover-v1\n"))
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return syncDir(root)
}

var (
	ErrCredentialRequired = errors.New("--credential-file is required; inherited HCOM_*/HERDER_*/HERDR_* values are hints, not authority; run `herder credential sweep` to issue any legacy seats before cutover")
	ErrStaleCredential    = errors.New("credential generation is not registry-current")
)

// File is the bounded versioned payload stored in one immutable token file.
type File struct {
	Version    int    `json:"version"`
	GUID       string `json:"guid"`
	Generation string `json:"generation"`
	Token      string `json:"token"`
}

// Selection is the identity selected by a successfully verified credential.
// AuditRef is safe to log: it contains guid+generation, never token material.
type Selection struct {
	GUID       string
	Generation string
	Path       string
	AuditRef   string
	Row        v2.SessionRecord
}

// Staged holds the per-seat rotation lock from durable staging through the
// registry flip and post-commit garbage collection.
type Staged struct {
	File File
	Path string
	lock *os.File
}

// CredentialPath returns the canonical immutable path for one generation.
func CredentialPath(registryPath, guid, generation string) string {
	return filepath.Join(filepath.Dir(registryPath), credentialDir, guid, generation+".token")
}

// CurrentPath resolves only non-secret path metadata from the registry.
func CurrentPath(registryPath, guid string) (string, error) {
	proj, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		return "", err
	}
	row := registry.V2ByGUID(proj, guid)
	if row == nil || row.State != v2.StateSeated || row.Seat == nil {
		return "", fmt.Errorf("guid %s is not currently seated", guid)
	}
	if row.Seat.CredentialGeneration == "" {
		return "", fmt.Errorf("guid %s is a legacy seat with no credential generation; run the issuance sweep or a completion-bearing recovery verb", guid)
	}
	return CredentialPath(registryPath, guid, row.Seat.CredentialGeneration), nil
}

// Stage durably creates a new immutable generation while holding the per-seat
// rotation lock. The caller must Close after the registry commit attempt.
func Stage(registryPath, guid string) (*Staged, error) {
	if guid == "" || guid == "." || guid == ".." || strings.ContainsAny(guid, `/\\`) || strings.ContainsRune(guid, '\x00') {
		return nil, fmt.Errorf("credential staging requires one safe guid, got %q", guid)
	}
	stateDir := filepath.Dir(registryPath)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	rootDir := filepath.Join(stateDir, credentialDir)
	if err := ensureOwnedDir(rootDir); err != nil {
		return nil, err
	}
	seatDir := filepath.Join(rootDir, guid)
	if err := ensureOwnedDir(seatDir); err != nil {
		return nil, err
	}
	lock, err := openOwnedRegular(filepath.Join(seatDir, ".rotate.lock"), unix.O_CREAT|unix.O_RDWR, 0o600, false)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	// Collect only generations that were already non-current when this later
	// completion began. The generation that this completion retires remains on
	// disk (but cannot authenticate after the registry flip) until another
	// completion takes this lock.
	currentGeneration := ""
	projection, loadErr := v2.LoadFile(registryPath, v2.LoadOptions{})
	if loadErr == nil {
		if current := registry.V2ByGUID(projection, guid); current != nil && current.Seat != nil {
			currentGeneration = current.Seat.CredentialGeneration
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		releaseLock(lock)
		return nil, fmt.Errorf("load registry before credential garbage collection: %w", loadErr)
	}
	if err := removeNonCurrent(seatDir, currentGeneration); err != nil {
		releaseLock(lock)
		return nil, fmt.Errorf("garbage-collect prior credential generations: %w", err)
	}
	generationBytes := make([]byte, 16)
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, generationBytes); err != nil {
		releaseLock(lock)
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		releaseLock(lock)
		return nil, err
	}
	record := File{
		Version:    Version,
		GUID:       guid,
		Generation: hex.EncodeToString(generationBytes),
		Token:      base64.RawURLEncoding.EncodeToString(tokenBytes),
	}
	path := CredentialPath(registryPath, guid, record.Generation)
	data, err := json.Marshal(record)
	if err != nil {
		releaseLock(lock)
		return nil, err
	}
	data = append(data, '\n')
	file, err := openOwnedRegular(path, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY, 0o600, true)
	if err != nil {
		releaseLock(lock)
		return nil, err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = syncDir(seatDir)
	}
	if err != nil {
		releaseLock(lock)
		return nil, err
	}
	return &Staged{File: record, Path: path, lock: lock}, nil
}

// Close releases the per-seat rotation lock after the registry commit attempt.
// Lazy garbage collection ran when this later Stage acquired the same lock, so
// a successful flip deliberately leaves the just-retired generation on disk.
func (s *Staged) Close(_ string, _ string) error {
	if s == nil || s.lock == nil {
		return nil
	}
	releaseLock(s.lock)
	s.lock = nil
	return nil
}

// Abort releases the rotation lock without deleting any staged or prior file.
// A later successful completion owns lazy orphan collection.
func (s *Staged) Abort() {
	if s == nil || s.lock == nil {
		return
	}
	releaseLock(s.lock)
	s.lock = nil
}

// Authenticate selects identity from the presented token before consulting
// any ambient correlate.
func Authenticate(registryPath, presentedPath string) (Selection, error) {
	if presentedPath == "" {
		return Selection{}, ErrCredentialRequired
	}
	presented, _, err := readCredential(presentedPath)
	if err != nil {
		return Selection{}, fmt.Errorf("read presented credential: %w", err)
	}
	proj, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		return Selection{}, fmt.Errorf("load registry: %w", err)
	}
	row := registry.V2ByGUID(proj, presented.GUID)
	if row == nil || row.State != v2.StateSeated || row.Seat == nil {
		return Selection{}, fmt.Errorf("credential guid %s is not currently seated", presented.GUID)
	}
	current := row.Seat.CredentialGeneration
	if current == "" {
		return Selection{}, fmt.Errorf("legacy seat %s has no credential generation; run the issuance sweep or a completion-bearing recovery verb", row.GUID)
	}
	if presented.Generation != current {
		return Selection{}, fmt.Errorf("%w for guid %s: presented %s, current %s", ErrStaleCredential, row.GUID, presented.Generation, current)
	}
	canonicalPath := CredentialPath(registryPath, row.GUID, current)
	canonical, _, err := readCredential(canonicalPath)
	if err != nil {
		return Selection{}, fmt.Errorf("current credential file is unavailable; run `herder repair reissue-credential --guid %s`: %w", row.GUID, err)
	}
	if canonical.Version != Version || canonical.GUID != row.GUID || canonical.Generation != current || subtle.ConstantTimeCompare([]byte(presented.Token), []byte(canonical.Token)) != 1 {
		return Selection{}, errors.New("presented credential does not match the registry-current canonical credential")
	}
	presentation := "canonical"
	if filepath.Clean(presentedPath) != filepath.Clean(canonicalPath) {
		presentation = "same-uid-copy"
	}
	if err := appendAudit(registryPath, row.GUID, current, presentation); err != nil {
		return Selection{}, fmt.Errorf("append credential audit: %w", err)
	}
	copy := *row
	return Selection{
		GUID: row.GUID, Generation: current, Path: canonicalPath,
		AuditRef: row.GUID + "/" + current, Row: copy,
	}, nil
}

func appendAudit(registryPath, guid, generation, presentation string) error {
	path := filepath.Join(filepath.Dir(registryPath), "credential-audit.jsonl")
	file, err := openOwnedRegular(path, unix.O_CREAT|unix.O_APPEND|unix.O_WRONLY, 0o600, true)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	entry := struct {
		Event        string `json:"event"`
		GUID         string `json:"guid"`
		Generation   string `json:"generation"`
		Presentation string `json:"presentation"`
		RecordedAt   string `json:"recorded_at"`
	}{"credential_authenticated", guid, generation, presentation, time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// VerifySelectedBus uses ambient correlates only to verify an already-selected
// credential row. It can refuse but can never return a different identity.
func VerifySelectedBus(rows []hcomidentity.Row, selected Selection, evidence hcomidentity.Evidence) error {
	if evidence.Name == "" && evidence.SessionID == "" && evidence.ProcessID == "" && len(evidence.PaneIDs) == 0 {
		return nil
	}
	resolved := hcomidentity.Resolve(rows, evidence)
	if !resolved.Verified {
		return fmt.Errorf("ambient correlate verification refused for credential-selected guid %s: %s", selected.GUID, resolved.Reason)
	}
	want := ""
	if selected.Row.Seat != nil {
		want = selected.Row.Seat.HcomName
	}
	if want == "" || resolved.Name != want {
		return fmt.Errorf("ambient correlates prove @%s but credential selected guid %s bound to @%s; refusing without re-selection", resolved.Name, selected.GUID, want)
	}
	return nil
}

// ExtractFlag removes one --credential-file PATH from argv before the verb's
// existing parser runs. It never accepts the token itself on argv.
func ExtractFlag(args []string) (string, []string, error) {
	path := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			out = append(out, args[i:]...)
			break
		}
		if args[i] != "--credential-file" {
			out = append(out, args[i])
			continue
		}
		if path != "" || i+1 >= len(args) || args[i+1] == "" {
			return "", nil, errors.New("--credential-file requires exactly one path")
		}
		path = args[i+1]
		i++
	}
	return path, out, nil
}

func readCredential(path string) (File, os.FileInfo, error) {
	file, err := openOwnedRegular(path, unix.O_RDONLY, 0, false)
	if err != nil {
		return File{}, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return File{}, nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return File{}, nil, fmt.Errorf("credential file %s is readable by group/other", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return File{}, nil, err
	}
	if len(data) > maxFileBytes {
		return File{}, nil, errors.New("credential file exceeds size limit")
	}
	var record File
	if err := json.Unmarshal(data, &record); err != nil {
		return File{}, nil, fmt.Errorf("decode credential: %w", err)
	}
	if record.Version != Version || record.GUID == "" || record.Generation == "" || record.Token == "" {
		return File{}, nil, errors.New("credential file is incomplete or has an unsupported version")
	}
	return record, info, nil
}

func openOwnedRegular(path string, flags int, mode uint32, allowEmpty bool) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, fmt.Errorf("refuse credential state %s: expected an effective-uid-owned regular file", path)
	}
	if !allowEmpty && info.Size() == 0 && flags&unix.O_ACCMODE == unix.O_RDONLY {
		file.Close()
		return nil, fmt.Errorf("credential state %s is empty", path)
	}
	return file, nil
}

func ensureOwnedDir(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("refuse credential directory %s: expected an effective-uid-owned real directory", path)
	}
	if info.Mode().Perm() != 0o700 {
		return os.Chmod(path, 0o700)
	}
	return nil
}

func removeNonCurrent(dir, current string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var joined error
	for _, entry := range entries {
		name := entry.Name()
		if name == ".rotate.lock" || filepath.Ext(name) != ".token" || strings.TrimSuffix(name, ".token") == current {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, err)
		}
	}
	if joined == nil {
		joined = syncDir(dir)
	}
	return joined
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func releaseLock(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}
