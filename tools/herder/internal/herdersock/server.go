package herdersock

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// ErrHeldByLiveServe is returned by Listen when another serve on the same
// state dir already answers on the socket. The caller audits one line and
// runs without a socket; its HTTP serve is unaffected.
var ErrHeldByLiveServe = errors.New("herder socket held by a live serve")

// Server owns one listening unix socket and unlinks it on Close.
type Server struct {
	path     string
	listener net.Listener
	answer   func(Request) Response
	audit    func(string, ...any)
	wg       sync.WaitGroup
	closed   chan struct{}
	once     sync.Once
}

// Listen binds <stateDir>/herder.sock (mode 0600) and answers each request
// with answer. A leftover file is probed first: a dial that connects means a
// live serve owns it (ErrHeldByLiveServe); a refused or missing target is a
// stale file and is replaced.
func Listen(stateDir string, answer func(Request) Response, audit func(string, ...any)) (*Server, error) {
	if audit == nil {
		audit = func(string, ...any) {}
	}
	path := Path(stateDir)
	if _, err := os.Lstat(path); err == nil {
		if probe, dialErr := net.DialTimeout("unix", path, ClientBudget); dialErr == nil {
			_ = probe.Close()
			return nil, ErrHeldByLiveServe
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	server := &Server{path: path, listener: listener, answer: answer, audit: audit, closed: make(chan struct{})}
	server.wg.Add(1)
	go server.accept()
	return server, nil
}

// Path is the bound socket path.
func (s *Server) Path() string { return s.path }

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
			default:
				s.audit("herder socket: accept failed: %v", err)
			}
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveOne(conn)
		}()
	}
}

// serveOne reads one request line and writes one response line.
func (s *Server) serveOne(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ServerDeadline))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var request Request
	response := Response{Error: "bad request"}
	if json.Unmarshal(line, &request) == nil {
		if request.Op == OpVitals {
			response = s.answer(request)
		} else {
			response = Response{Error: "unknown op"}
		}
	}
	_ = json.NewEncoder(conn).Encode(response)
}

// Close stops accepting, waits for in-flight replies and unlinks the socket
// so a CLI sees "absent" (no serve) rather than "stale" afterwards.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.closed)
		_ = s.listener.Close()
		s.wg.Wait()
		_ = os.Remove(s.path)
	})
}
