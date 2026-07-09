package observercmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type readErrorConn struct {
	err error
}

func (c readErrorConn) Read([]byte) (int, error)         { return 0, c.err }
func (c readErrorConn) Write([]byte) (int, error)        { return 0, io.ErrClosedPipe }
func (c readErrorConn) Close() error                     { return nil }
func (c readErrorConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c readErrorConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c readErrorConn) SetDeadline(time.Time) error      { return nil }
func (c readErrorConn) SetReadDeadline(time.Time) error  { return nil }
func (c readErrorConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return "test" }
func (a dummyAddr) String() string  { return string(a) }

func TestReadLoopLogsScannerErrorsAndFlushesPending(t *testing.T) {
	var log bytes.Buffer
	pending := make(chan socketResponse, 1)
	client := &herdrSocketClient{
		conn:   readErrorConn{err: errors.New("forced read failure")},
		errLog: &log,
		pending: map[string]chan socketResponse{
			"1": pending,
		},
		events: make(chan json.RawMessage, 1),
		closed: make(chan struct{}),
	}

	client.readLoop()

	if got := log.String(); !strings.Contains(got, "read loop ended: forced read failure") {
		t.Fatalf("readLoop did not log scanner error; log=%q", got)
	}
	resp := <-pending
	if resp.err == nil || !strings.Contains(resp.err.Error(), "forced read failure") {
		t.Fatalf("pending call was not flushed with read error; err=%v", resp.err)
	}
}
