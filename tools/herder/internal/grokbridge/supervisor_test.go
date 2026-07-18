package grokbridge

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorLeaseRefusesSecondSupervisorForSeat(t *testing.T) {
	state := t.TempDir()
	first, err := acquireSupervisorLease(state, "seat")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err = acquireSupervisorLease(state, "seat"); !errors.Is(err, ErrSupervisorAlreadyRunning) {
		t.Fatalf("second supervisor lease error=%v, want already-running refusal", err)
	}
	identity, err := readSupervisorIdentity(supervisorIdentityPath(state, "seat"))
	if err != nil || identity.PID != os.Getpid() || identity.StartTime == "" {
		t.Fatalf("supervisor identity=%+v err=%v", identity, err)
	}
	first.Close()
	first = nil
	third, err := acquireSupervisorLease(state, "seat")
	if err != nil {
		t.Fatalf("lease was not released: %v", err)
	}
	third.Close()
}

func TestConcurrentSupervisorStartLeavesOneSupervisorAndOneChild(t *testing.T) {
	state := t.TempDir()
	starts := filepath.Join(t.TempDir(), "starts")
	child := filepath.Join(t.TempDir(), "bridge-child")
	script := "#!/bin/sh\nprintf '%s\\n' start >> \"$HERDER_TEST_STARTS\"\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(child, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_TEST_STARTS", starts)
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan int, 1)
	go func() {
		firstDone <- superviseBridgeContext(ctx, []string{"--supervise"}, state, "seat", false, child, io.Discard)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, _ := os.ReadFile(starts)
		if strings.Count(string(data), "start") == 1 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("first supervisor did not start one child: %q", data)
		}
		time.Sleep(time.Millisecond)
	}
	if rc := superviseBridgeContext(context.Background(), []string{"--supervise"}, state, "seat", false, child, io.Discard); rc != 23 {
		cancel()
		t.Fatalf("second supervisor rc=%d, want already-running refusal 23", rc)
	}
	data, err := os.ReadFile(starts)
	if err != nil || strings.Count(string(data), "start") != 1 {
		cancel()
		t.Fatalf("concurrent supervisors started duplicate children: %q err=%v", data, err)
	}
	cancel()
	select {
	case rc := <-firstDone:
		if rc != 0 {
			t.Fatalf("first supervisor stop rc=%d", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first supervisor did not stop")
	}
}

func TestStaleSupervisorIncarnationCannotAuthorizeSignal(t *testing.T) {
	current, err := inspectSupervisorPID(os.Getpid(), t.TempDir(), "seat", false)
	if err != nil {
		t.Fatal(err)
	}
	current.StartTime = "stale-" + current.StartTime
	if supervisorAlive(current) {
		t.Fatal("stale process incarnation was accepted as live supervisor")
	}
	if err = signalSupervisor(current, syscall.SIGTERM); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("stale signal error=%v, want ESRCH refusal", err)
	}
}

func TestSupervisorArgsRequireExactSupervisorShape(t *testing.T) {
	seat, state, ok := supervisorArgs([]string{"/bin/herder", "grok", "bridge", "--seat", "seat-a", "--state-dir", "/state", "--supervise"})
	if !ok || seat != "seat-a" || state != "/state" {
		t.Fatalf("supervisor args=(%q,%q,%t)", seat, state, ok)
	}
	for _, args := range [][]string{
		{"/bin/herder", "grok", "bridge", "--seat", "seat-a", "--state-dir", "/state", "--supervise", "--child"},
		{"/bin/herder", "other", "--seat", "seat-a", "--state-dir", "/state", "--supervise"},
	} {
		if _, _, ok := supervisorArgs(args); ok {
			t.Fatalf("accepted non-supervisor argv: %v", args)
		}
	}
}

func TestStopSeatSupervisorsStopsSupervisorAndChildWithinBoundedWindow(t *testing.T) {
	state := t.TempDir()
	seat := "bounded-seat"
	cmd := startSupervisorFixture(t, state, seat)
	childPID := waitFixtureChildPID(t, state, seat)
	started := time.Now()
	result, err := StopSeatSupervisors(state, seat, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.Termed != 1 || time.Since(started) > time.Second {
		t.Fatalf("bounded stop result=%+v elapsed=%s", result, time.Since(started))
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("supervisor exit: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for syscall.Kill(childPID, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err = syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("bridge child pid %d survived supervisor group stop: %v", childPID, err)
	}
}

func TestRowlessSupervisorIsReportedButNeverAutoKilled(t *testing.T) {
	state := t.TempDir()
	cmd := startSupervisorFixture(t, state, "rowless-seat")
	findings, err := SweepOrphanSupervisors(filepath.Join(state, "registry.jsonl"), time.Now(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Type != "rowless-grok-bridge-orphan" || !strings.Contains(findings[0].Suggested, "stop-bridge") {
		t.Fatalf("rowless findings=%+v", findings)
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("rowless supervisor was killed automatically: %v", err)
	}
}

func TestSeatedSupervisorIsNeverOrphanCandidate(t *testing.T) {
	state := t.TempDir()
	seedGrokRegistryRow(t, state, "live-seat", "owner")
	cmd := startSupervisorFixture(t, state, "live-seat")
	findings, err := SweepOrphanSupervisors(filepath.Join(state, "registry.jsonl"), time.Now(), time.Millisecond)
	if err != nil || len(findings) != 0 {
		t.Fatalf("live-seat findings=%+v err=%v", findings, err)
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("seated supervisor was killed: %v", err)
	}
}

func TestNonSeatedSupervisorRequiresGraceThenStops(t *testing.T) {
	state := t.TempDir()
	seat := "dead-seat"
	seedGrokRegistryRow(t, state, seat, "owner")
	retireGrokRegistryRow(t, state, seat)
	cmd := startSupervisorFixture(t, state, seat)
	now := time.Now().UTC()
	findings, err := SweepOrphanSupervisors(filepath.Join(state, "registry.jsonl"), now, time.Millisecond)
	if err != nil || len(findings) != 1 || findings[0].Type != "grok-bridge-orphan-grace" {
		t.Fatalf("first sweep findings=%+v err=%v", findings, err)
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("supervisor died before grace: %v", err)
	}
	findings, err = SweepOrphanSupervisors(filepath.Join(state, "registry.jsonl"), now.Add(2*time.Millisecond), time.Millisecond)
	if err != nil || len(findings) != 1 || findings[0].Type != "grok-bridge-orphan-reaped" {
		t.Fatalf("second sweep findings=%+v err=%v", findings, err)
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		t.Fatalf("TERM-stopped supervisor exit: %v", waitErr)
	}
}

func TestNonSeatedSupervisorWithLiveClientIsNeverReaped(t *testing.T) {
	state := t.TempDir()
	seat := "client-seat"
	seedGrokRegistryRow(t, state, seat, "owner")
	retireGrokRegistryRow(t, state, seat)
	cmd := startSupervisorFixture(t, state, seat)
	bridge := startMockBridgeForSeat(t, state, seat, "owner")
	defer bridge.close()
	tap := connectTap(t, bridge.b.socket, "owner")
	defer tap.close()

	findings, err := SweepOrphanSupervisors(filepath.Join(state, "registry.jsonl"), time.Now(), time.Millisecond)
	if err != nil || len(findings) != 1 || findings[0].Type != "grok-bridge-orphan-refused" || !strings.Contains(findings[0].Detail, "authenticated client") {
		t.Fatalf("live-client findings=%+v err=%v", findings, err)
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("supervisor with live client was killed: %v", err)
	}
}

func startSupervisorFixture(t *testing.T, state, seat string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestSupervisorFixtureProcess", "--", "grok", "bridge", "--seat", seat, "--state-dir", state, "--supervise")
	cmd.Env = append(os.Environ(), "HERDER_TEST_SUPERVISOR_FIXTURE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		processes, _ := DiscoverSupervisors(state)
		if len(filterSeat(processes, seat)) == 1 {
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervisor fixture did not become discoverable: seat=%s", seat)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSupervisorFixtureProcess(t *testing.T) {
	if os.Getenv("HERDER_TEST_SUPERVISOR_FIXTURE") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		os.Exit(90)
	}
	seat, state, ok := supervisorArgs(os.Args[separator+1:])
	if !ok {
		os.Exit(91)
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		os.Exit(92)
	}
	dir := SeatDir(state, seat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		os.Exit(93)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture-child.pid"), []byte(strconv.Itoa(child.Process.Pid)+"\n"), 0o600); err != nil {
		os.Exit(94)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	<-signals
	os.Exit(0)
}

func waitFixtureChildPID(t *testing.T, state, seat string) int {
	t.Helper()
	path := filepath.Join(SeatDir(state, seat), "fixture-child.pid")
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 1 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture child pid unavailable: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}
