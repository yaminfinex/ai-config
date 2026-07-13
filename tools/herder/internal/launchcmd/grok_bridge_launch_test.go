package launchcmd

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("HERDER_TEST_GROK_BRIDGE_HELPER") == "1" {
		os.Exit(runGrokBridgeLaunchHelper())
	}
	os.Exit(m.Run())
}

func runGrokBridgeLaunchHelper() int {
	state, seat := "", ""
	for i := 1; i+1 < len(os.Args); i++ {
		switch os.Args[i] {
		case "--state-dir":
			state = os.Args[i+1]
		case "--seat":
			seat = os.Args[i+1]
		}
	}
	if state == "" || seat == "" {
		return 91
	}
	if err := os.WriteFile(os.Getenv("HERDER_TEST_GROK_BRIDGE_ARGS"), []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600); err != nil {
		return 92
	}
	dir := filepath.Join(state, "grok", seat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 93
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "bridge.sock"))
	if err != nil {
		return 94
	}
	defer listener.Close()
	if err = os.WriteFile(filepath.Join(dir, "bus-name"), []byte("manual-test-bus\n"), 0o600); err != nil {
		return 95
	}
	if err = os.WriteFile(os.Getenv("HERDER_TEST_GROK_BRIDGE_PID"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return 96
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	<-signals
	return 0
}

func TestManualLaunchPassesRetireOnStopToBridgeSupervisor(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "bridge-args")
	pidPath := filepath.Join(root, "bridge-pid")
	plan := grokLaunchPlan{
		StateDir:  root,
		Seat:      "manual-seat",
		HcomBin:   "/bin/true",
		SessionID: "019f36ef-acb5-72a2-820c-cbcbbdd1c413",
		Env: replaceLaunchEnv(os.Environ(), map[string]string{
			"HERDER_TEST_GROK_BRIDGE_HELPER": "1",
			"HERDER_TEST_GROK_BRIDGE_ARGS":   argsPath,
			"HERDER_TEST_GROK_BRIDGE_PID":    pidPath,
		}),
	}
	name, err := startGrokBridge(plan, true)
	if err != nil || name != "manual-test-bus" {
		t.Fatalf("manual bridge name=%q err=%v", name, err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(strings.Fields(string(data)), "--retire-on-stop") {
		t.Fatalf("manual bridge argv omitted --retire-on-stop:\n%s", data)
	}
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatalf("stop helper bridge: %v", err)
	}
}
