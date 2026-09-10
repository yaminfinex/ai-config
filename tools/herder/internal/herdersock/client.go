package herdersock

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

// Ask sends one request to the serve behind stateDir and returns its answer.
// The bool is "a live serve answered with a hit". It never returns an error:
// every failure is a miss and the caller reads directly. The three no-serve
// cases and their costs:
//
//   - no serve: the socket file is absent (the serve unlinks it on every
//     exit) → one failed stat, well under a millisecond, no dial;
//   - stale socket (serve died without cleanup): the dial is refused → the
//     file is unlinked here, one refused connect, under a millisecond;
//   - hung serve (accepts, never answers): bounded by ClientBudget on the
//     write plus the read of one line. This is the only path that waits.
func Ask(stateDir string, request Request) (Response, bool) {
	path := Path(stateDir)
	if _, err := os.Stat(path); err != nil {
		return Response{Miss: true}, false
	}
	conn, err := net.DialTimeout("unix", path, ClientBudget)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			_ = os.Remove(path)
		}
		return Response{Miss: true}, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ClientBudget))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{Miss: true}, false
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Response{Miss: true}, false
	}
	var response Response
	if json.Unmarshal(line, &response) != nil || response.Miss || response.Error != "" {
		return Response{Miss: true}, false
	}
	return response, true
}
