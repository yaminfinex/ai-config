package grokbridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type Client struct {
	mu         sync.Mutex
	socket     string
	generation uint64
	sessionID  string
}

func DialClient(socket string) (*Client, error) {
	return dialClient(socket, processCapability("HERDER_GROK_SESSION_ID"))
}

// DialClientForSession presents explicit owning-session evidence. Lifecycle
// and cull callers use it so generation-fenced bridge operations never depend
// on temporarily mutating process-wide environment state.
func DialClientForSession(socket, sessionID string) (*Client, error) {
	return dialClient(socket, sessionID)
}

func dialClient(socket, sessionID string) (*Client, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("connect seat bridge: %w", err)
	}
	defer c.Close()
	if err = json.NewEncoder(c).Encode(Request{Op: "handshake", SessionID: sessionID}); err != nil {
		return nil, err
	}
	var r Response
	if err = json.NewDecoder(c).Decode(&r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("%s", r.Error)
	}
	return &Client{socket: socket, generation: r.Generation, sessionID: sessionID}, nil
}

func Tap(socket string, stdout io.Writer) error {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return fmt.Errorf("connect seat bridge: %w", err)
	}
	defer c.Close()
	if err = json.NewEncoder(c).Encode(Request{Op: "tap", SessionID: processCapability("HERDER_GROK_SESSION_ID")}); err != nil {
		return err
	}
	br := bufio.NewReader(c)
	var hello Response
	line, err := br.ReadBytes('\n')
	if err != nil {
		return err
	}
	if err = json.Unmarshal(line, &hello); err != nil {
		return err
	}
	if !hello.OK {
		return fmt.Errorf("%s", hello.Error)
	}
	_, err = io.Copy(stdout, br)
	return err
}

func (c *Client) Call(req Request) (Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, err := c.call(req)
	if err == nil || !strings.HasPrefix(resp.Error, "stale bridge generation ") {
		return resp, err
	}
	fresh, reconnectErr := dialClient(c.socket, c.sessionID)
	if reconnectErr != nil {
		return resp, fmt.Errorf("%v; reconnect seat bridge: %w", err, reconnectErr)
	}
	c.generation = fresh.generation
	return c.call(req)
}

func (c *Client) call(req Request) (Response, error) {
	req.Generation = c.generation
	req.SessionID = c.sessionID
	return roundTrip(c.socket, req)
}

func processCapability(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	pid := os.Getppid()
	for hops := 0; pid > 1 && hops < 32; hops++ {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid)); err == nil {
			for _, item := range strings.Split(string(data), "\x00") {
				if value, ok := strings.CutPrefix(item, name+"="); ok && value != "" {
					return value
				}
			}
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			break
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			break
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) < 2 {
			break
		}
		next, err := strconv.Atoi(fields[1])
		if err != nil || next == pid {
			break
		}
		pid = next
	}
	return ""
}
