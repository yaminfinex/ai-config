package sidecarcmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

type options struct {
	tool string
}

type hcomRow struct {
	Name          string           `json:"name"`
	Tag           string           `json:"tag"`
	Directory     string           `json:"directory"`
	Tool          string           `json:"tool"`
	Status        string           `json:"status"`
	SessionID     string           `json:"session_id"`
	CreatedAt     flexibleJSONText `json:"created_at"`
	LaunchContext struct {
		PaneID string `json:"pane_id"`
	} `json:"launch_context"`
}

type flexibleJSONText string

func (t *flexibleJSONText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = flexibleJSONText(s)
		return nil
	}
	*t = flexibleJSONText(string(b))
	return nil
}

// Run bridges hcom status to herdr's pane.report_agent socket protocol.
func Run(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	_ = stderr
	opts, ok := parseArgs(args)
	if !ok {
		return 1
	}
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PANE_ID") == "" {
		return 0
	}
	sidecar := &sidecar{
		tool:       opts.tool,
		paneID:     os.Getenv("HERDR_PANE_ID"),
		socketPath: os.Getenv("HERDR_SOCKET_PATH"),
		tag:        os.Getenv("HERDER_ROLE"),
		cwd:        currentCWD(),
		ppid0:      os.Getppid(),
	}
	return sidecar.run()
}

func parseArgs(args []string) (options, bool) {
	var opts options
	for i := 0; i < len(args); {
		switch args[i] {
		case "--tool":
			if i+1 >= len(args) {
				return opts, false
			}
			opts.tool = args[i+1]
			i += 2
		default:
			return opts, false
		}
	}
	return opts, opts.tool != ""
}

type sidecar struct {
	tool       string
	paneID     string
	socketPath string
	tag        string
	cwd        string
	ppid0      int
	lastState  string
	missing    int
}

func (s *sidecar) run() int {
	s.report("working")
	s.lastState = "working"

	row := s.discoverRow()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if os.Getppid() != s.ppid0 {
			s.release()
			return 0
		}
		if row == nil {
			s.missing++
		} else {
			s.missing = 0
			if state, ok := mapStatus(row.Status); ok && state != s.lastState {
				s.report(state)
				s.lastState = state
			}
		}
		if s.missing >= 5 {
			s.release()
			return 0
		}
		<-ticker.C
		row = s.findRow(hcomList())
	}
}

func (s *sidecar) discoverRow() *hcomRow {
	for i := 0; i < 90; i++ {
		if os.Getppid() != s.ppid0 {
			s.release()
			return nil
		}
		if row := s.findRow(hcomList()); row != nil {
			return row
		}
		time.Sleep(700 * time.Millisecond)
	}
	return nil
}

func mapStatus(status string) (string, bool) {
	switch status {
	case "active":
		return "working", true
	case "listening":
		return "idle", true
	case "blocked":
		return "blocked", true
	default:
		return "", false
	}
}

func findRowForPane(rows []hcomRow, paneID string) *hcomRow {
	for i := range rows {
		if rows[i].LaunchContext.PaneID == paneID {
			return &rows[i]
		}
	}
	return nil
}

func (s *sidecar) findRow(rows []hcomRow) *hcomRow {
	if row := findRowForPane(rows, s.paneID); row != nil {
		return row
	}
	return findRowForLaunchFallback(rows, s.tool, s.tag, s.cwd)
}

func findRowForLaunchFallback(rows []hcomRow, tool, tag, cwd string) *hcomRow {
	if tool == "" || tag == "" || cwd == "" {
		return nil
	}
	var hit *hcomRow
	for i := range rows {
		if rows[i].Tool == tool && rows[i].Tag == tag && rows[i].Directory == cwd {
			hit = &rows[i]
		}
	}
	return hit
}

func currentCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func hcomList() []hcomRow {
	cmd := exec.Command("hcom", "list", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	var rows []hcomRow
	if json.Unmarshal(stdout.Bytes(), &rows) != nil {
		return nil
	}
	return rows
}

func (s *sidecar) report(state string) {
	_ = s.send("pane.report_agent", map[string]any{
		"pane_id": s.paneID,
		"source":  "herder:sidecar",
		"agent":   s.tool,
		"state":   state,
		"seq":     time.Now().UnixNano(),
	})
}

func (s *sidecar) release() {
	_ = s.send("pane.release_agent", map[string]any{
		"pane_id": s.paneID,
		"source":  "herder:sidecar",
		"agent":   s.tool,
		"seq":     time.Now().UnixNano(),
	})
}

func (s *sidecar) send(method string, params map[string]any) error {
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	req := map[string]any{
		"id":     fmt.Sprintf("herder:sidecar:%d", time.Now().UnixNano()),
		"method": method,
		"params": params,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	_, _ = bufio.NewReader(conn).ReadBytes('\n')
	return nil
}
