package grokbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const DefaultSupervisorStopTimeout = 2 * time.Second

var ErrSupervisorAlreadyRunning = errors.New("Grok bridge supervisor is already running for this seat")

type SupervisorProcess struct {
	PID       int    `json:"pid"`
	PGID      int    `json:"pgid"`
	StartTime string `json:"start_time"`
	Seat      string `json:"seat"`
	StateDir  string `json:"state_dir"`
	Child     bool   `json:"child,omitempty"`
}

type StopResult struct {
	Matched         int
	Termed          int
	Killed          int
	ChildrenMatched int
	ChildrenTermed  int
	ChildrenKilled  int
}

type supervisorLease struct {
	lock     *os.File
	identity SupervisorProcess
	path     string
}

func supervisorLockPath(stateDir, seat string) string {
	return filepath.Join(SeatDir(stateDir, seat), "supervisor.lock")
}

func supervisorIdentityPath(stateDir, seat string) string {
	return filepath.Join(SeatDir(stateDir, seat), "supervisor.json")
}

func acquireSupervisorLease(stateDir, seat string) (*supervisorLease, error) {
	dir := SeatDir(stateDir, seat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(supervisorLockPath(stateDir, seat), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, ErrSupervisorAlreadyRunning
	}
	identity, err := inspectSupervisorPID(os.Getpid(), stateDir, seat, false)
	if err != nil {
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
		lock.Close()
		return nil, err
	}
	data, err := json.Marshal(identity)
	if err == nil {
		err = writeAtomic(supervisorIdentityPath(stateDir, seat), append(data, '\n'), 0o600)
	}
	if err != nil {
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
		lock.Close()
		return nil, err
	}
	return &supervisorLease{lock: lock, identity: identity, path: supervisorIdentityPath(stateDir, seat)}, nil
}

func (l *supervisorLease) Close() {
	if l == nil {
		return
	}
	if current, err := readSupervisorIdentity(l.path); err == nil && sameSupervisor(current, l.identity) {
		_ = os.Remove(l.path)
	}
	_ = syscall.Flock(int(l.lock.Fd()), syscall.LOCK_UN)
	_ = l.lock.Close()
}

func readSupervisorIdentity(path string) (SupervisorProcess, error) {
	var identity SupervisorProcess
	info, err := os.Lstat(path)
	if err != nil {
		return identity, err
	}
	if !info.Mode().IsRegular() {
		return identity, fmt.Errorf("refuse bridge supervisor identity %s: not a regular file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) {
		return identity, fmt.Errorf("refuse bridge supervisor identity %s: owner does not match effective uid", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return identity, err
	}
	if err = json.Unmarshal(data, &identity); err != nil {
		return identity, err
	}
	return identity, nil
}

func DiscoverSupervisors(stateDir string) ([]SupervisorProcess, error) {
	return discoverBridgeProcesses(stateDir, false, func(args []string) (string, string, bool) {
		return supervisorArgs(args)
	})
}

func discoverBridgeChildren(stateDir string) ([]SupervisorProcess, error) {
	return discoverBridgeProcesses(stateDir, true, func(args []string) (string, string, bool) {
		return bridgeChildArgs(args)
	})
}

func discoverBridgeProcesses(stateDir string, child bool, match func([]string) (string, string, bool)) ([]SupervisorProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	cleanState := filepath.Clean(stateDir)
	var found []SupervisorProcess
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		args, err := readProcessArgs(pid)
		if err != nil {
			continue
		}
		seat, processState, ok := match(args)
		if !ok || filepath.Clean(processState) != cleanState {
			continue
		}
		identity, err := inspectBridgePID(pid, cleanState, seat, child, true)
		if err == nil {
			found = append(found, identity)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Seat == found[j].Seat {
			return found[i].PID < found[j].PID
		}
		return found[i].Seat < found[j].Seat
	})
	return found, nil
}

func StopSeatSupervisors(stateDir, seat string, timeout time.Duration) (StopResult, error) {
	var result StopResult
	if timeout <= 0 {
		timeout = DefaultSupervisorStopTimeout
	}
	all, err := DiscoverSupervisors(stateDir)
	if err != nil {
		return result, err
	}
	allChildren, err := discoverBridgeChildren(stateDir)
	if err != nil {
		return result, err
	}
	var targets []SupervisorProcess
	for _, process := range all {
		if process.Seat == seat {
			targets = append(targets, process)
		}
	}
	children := filterProcessesBySeat(allChildren, seat)
	result.Matched = len(targets)
	result.ChildrenMatched = len(children)
	for _, target := range targets {
		if !supervisorAlive(target) {
			continue
		}
		if err := signalSupervisor(target, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return result, err
		}
		result.Termed++
	}
	waitProcessesGone(targets, timeout)
	currentChildren, discoverErr := discoverBridgeChildren(stateDir)
	if discoverErr != nil {
		return result, discoverErr
	}
	children = mergeProcesses(children, filterProcessesBySeat(currentChildren, seat))
	result.ChildrenMatched = len(children)
	for _, child := range children {
		if !supervisorAlive(child) {
			continue
		}
		if err := syscall.Kill(child.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return result, err
		}
		result.ChildrenTermed++
	}
	waitProcessesGone(children, timeout)
	for _, target := range targets {
		if !supervisorAlive(target) {
			continue
		}
		if err := signalSupervisor(target, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return result, err
		}
		result.Killed++
	}
	for _, child := range children {
		if !supervisorAlive(child) {
			continue
		}
		if err := syscall.Kill(child.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return result, err
		}
		result.ChildrenKilled++
	}
	waitProcessesGone(append(append([]SupervisorProcess(nil), targets...), children...), timeout)
	remainingSupervisors, err := DiscoverSupervisors(stateDir)
	if err != nil {
		return result, err
	}
	remainingChildren, err := discoverBridgeChildren(stateDir)
	if err != nil {
		return result, err
	}
	if len(filterProcessesBySeat(remainingSupervisors, seat)) != 0 || len(filterProcessesBySeat(remainingChildren, seat)) != 0 {
		return result, fmt.Errorf("bridge processes for seat %s remained alive after TERM and KILL", seat)
	}
	if err := verifyBridgeSocketStopped(stateDir, seat); err != nil {
		return result, err
	}
	_ = os.Remove(supervisorIdentityPath(stateDir, seat))
	return result, nil
}

func waitProcessesGone(processes []SupervisorProcess, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if supervisorsGone(processes) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return supervisorsGone(processes)
}

func filterProcessesBySeat(processes []SupervisorProcess, seat string) []SupervisorProcess {
	var filtered []SupervisorProcess
	for _, process := range processes {
		if process.Seat == seat {
			filtered = append(filtered, process)
		}
	}
	return filtered
}

func mergeProcesses(groups ...[]SupervisorProcess) []SupervisorProcess {
	seen := map[string]bool{}
	var merged []SupervisorProcess
	for _, group := range groups {
		for _, process := range group {
			key := fmt.Sprintf("%d:%s", process.PID, process.StartTime)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, process)
			}
		}
	}
	return merged
}

func verifyBridgeSocketStopped(stateDir, seat string) error {
	path := SocketPath(stateDir, seat)
	conn, err := net.DialTimeout("unix", path, 50*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("bridge socket for seat %s still accepts clients after process teardown", seat)
	}
	info, statErr := os.Lstat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil {
		return statErr
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refuse stale bridge socket cleanup for seat %s: %s is not a Unix socket", seat, path)
	}
	return os.Remove(path)
}

func signalSupervisor(process SupervisorProcess, signal syscall.Signal) error {
	if !supervisorAlive(process) {
		return syscall.ESRCH
	}
	if process.PGID == process.PID && process.PGID > 1 {
		return syscall.Kill(-process.PGID, signal)
	}
	return syscall.Kill(process.PID, signal)
}

func supervisorsGone(processes []SupervisorProcess) bool {
	for _, process := range processes {
		if supervisorAlive(process) {
			return false
		}
	}
	return true
}

func supervisorAlive(process SupervisorProcess) bool {
	current, err := inspectBridgePID(process.PID, process.StateDir, process.Seat, process.Child, true)
	return err == nil && sameSupervisor(current, process)
}

func sameSupervisor(a, b SupervisorProcess) bool {
	return a.PID == b.PID && a.PGID == b.PGID && a.StartTime == b.StartTime && a.Seat == b.Seat && a.Child == b.Child && filepath.Clean(a.StateDir) == filepath.Clean(b.StateDir)
}

func inspectSupervisorPID(pid int, stateDir, seat string, requireArgs bool) (SupervisorProcess, error) {
	return inspectBridgePID(pid, stateDir, seat, false, requireArgs)
}

func inspectBridgePID(pid int, stateDir, seat string, child, requireArgs bool) (SupervisorProcess, error) {
	start, err := processStartTime(pid)
	if err != nil {
		return SupervisorProcess{}, err
	}
	if requireArgs {
		args, err := readProcessArgs(pid)
		if err != nil {
			return SupervisorProcess{}, err
		}
		gotSeat, gotState, ok := supervisorArgs(args)
		if child {
			gotSeat, gotState, ok = bridgeChildArgs(args)
		}
		if !ok || gotSeat != seat || filepath.Clean(gotState) != filepath.Clean(stateDir) {
			return SupervisorProcess{}, errors.New("process argv no longer identifies the expected bridge supervisor")
		}
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return SupervisorProcess{}, err
	}
	return SupervisorProcess{PID: pid, PGID: pgid, StartTime: start, Seat: seat, StateDir: filepath.Clean(stateDir), Child: child}, nil
}

func processStartTime(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", errors.New("malformed process stat")
	}
	fields := strings.Fields(string(data)[end+1:])
	if len(fields) <= 19 {
		return "", errors.New("process stat omits start time")
	}
	if fields[0] == "Z" {
		return "", os.ErrProcessDone
	}
	return fields[19], nil
}

func readProcessArgs(pid int) ([]string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.New("empty process argv")
	}
	return parts, nil
}

func supervisorArgs(args []string) (seat, stateDir string, ok bool) {
	seat, stateDir, supervise, child, command := bridgeProcessArgs(args)
	return seat, stateDir, command && supervise && !child && seat != "" && stateDir != ""
}

func bridgeChildArgs(args []string) (seat, stateDir string, ok bool) {
	seat, stateDir, supervise, child, command := bridgeProcessArgs(args)
	return seat, stateDir, command && child && !supervise && seat != "" && stateDir != ""
}

func bridgeProcessArgs(args []string) (seat, stateDir string, supervise, child, command bool) {
	for i := 0; i < len(args); i++ {
		if i+1 < len(args) && args[i] == "grok" && args[i+1] == "bridge" {
			command = true
		}
		switch args[i] {
		case "--seat":
			if i+1 < len(args) {
				seat = args[i+1]
				i++
			}
		case "--state-dir":
			if i+1 < len(args) {
				stateDir = args[i+1]
				i++
			}
		case "--supervise":
			supervise = true
		case "--child":
			child = true
		}
	}
	return seat, stateDir, supervise, child, command
}
