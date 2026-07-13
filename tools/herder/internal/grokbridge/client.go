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
)

func currentGeneration(socket string) (uint64, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return 0, fmt.Errorf("connect seat bridge: %w", err)
	}
	defer c.Close()
	if err = json.NewEncoder(c).Encode(Request{Op: "handshake", SessionID: processCapability("HERDER_GROK_SESSION_ID")}); err != nil {
		return 0, err
	}
	var r Response
	if err = json.NewDecoder(c).Decode(&r); err != nil {
		return 0, err
	}
	if !r.OK {
		return 0, fmt.Errorf("%s", r.Error)
	}
	return r.Generation, nil
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
	if err = json.NewDecoder(br).Decode(&hello); err != nil {
		return err
	}
	if !hello.OK {
		return fmt.Errorf("%s", hello.Error)
	}
	_, err = io.Copy(stdout, br)
	return err
}

func Call(socket string, req Request) (Response, error) {
	gen, err := currentGeneration(socket)
	if err != nil {
		return Response{}, err
	}
	req.Generation = gen
	req.SessionID = processCapability("HERDER_GROK_SESSION_ID")
	return roundTrip(socket, req)
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
