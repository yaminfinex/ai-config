package send

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	cullDeliveryVerifyMax = 3 * time.Second
	cullCommandWaitDelay  = 100 * time.Millisecond
)

// CullRequest is the cull-only request envelope. Full names drive --name and
// routing; roster-derived base names match the identity hcom stamps on message
// events. This path never invents or parses either identity form.
type CullRequest struct {
	Sender     string
	SenderBase string
	Target     string
	TargetBase string
	BusDir     string
	Thread     string
	Message    string
	Deadline   time.Time
}

// CullDelivery reports whether the one permitted notice send was received or
// queued. NoticeFloor is the bus event high-water mark from before the send;
// it prevents an older event from satisfying this cull's acknowledgement.
type CullDelivery struct {
	Verdict     string
	NoticeID    int64
	NoticeFloor int64
}

type cullBusEvent struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Data struct {
		From         string   `json:"from"`
		Intent       string   `json:"intent"`
		Thread       string   `json:"thread"`
		ReplyTo      string   `json:"reply_to"`
		ReplyToLocal int64    `json:"reply_to_local"`
		Context      string   `json:"context"`
		Mentions     []string `json:"mentions"`
		DeliveredTo  []string `json:"delivered_to"`
	} `json:"data"`
}

// DeliverCullRequest reuses the verified-delivery spine for cull while adding
// the request/thread envelope and a deadline-bounded send-window lock. It
// sends once. A queued verdict is success: the caller must keep waiting for
// the target's later protocol acknowledgement.
func DeliverCullRequest(req CullRequest) CullDelivery {
	if req.Sender == "" || req.SenderBase == "" || req.Target == "" || req.TargetBase == "" || req.Thread == "" || !time.Now().Before(req.Deadline) {
		return CullDelivery{Verdict: "send_failed"}
	}
	env := cullBusEnv(req.BusDir)
	ctx, cancel := context.WithDeadline(context.Background(), req.Deadline)
	defer cancel()
	if rc := runCullCommand(ctx, env, "list", req.Target); rc != 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return CullDelivery{Verdict: "roster_timeout"}
		}
		return CullDelivery{Verdict: "not_joined"}
	}

	unlock, err := lockSendWindowUntil(req.BusDir, req.Sender, req.Target, req.Deadline)
	if err != nil {
		return CullDelivery{Verdict: "send_failed"}
	}
	defer unlock()

	preOut, _ := outputCullCommand(ctx, env, "events", "--full", "--last", "100")
	floor := maxCullEventID(preOut)
	receiptArgs := []string{"events", "--full", "--last", "50", "--agent", req.Target, "--context", "deliver:" + req.Sender}
	preReceiptOut, _ := outputCullCommand(ctx, env, receiptArgs...)
	receiptFloor := maxCullEventID(preReceiptOut)
	args := []string{"send", "--name", req.Sender, "@" + req.Target, "--intent", "request", "--thread", req.Thread, "--", req.Message}
	if rc := runCullCommand(ctx, env, args...); rc != 0 {
		return CullDelivery{Verdict: "send_failed", NoticeFloor: floor}
	}

	remaining := time.Until(req.Deadline)
	verifyFor := remaining / 4
	if verifyFor > cullDeliveryVerifyMax {
		verifyFor = cullDeliveryVerifyMax
	}
	if verifyFor <= 0 {
		return CullDelivery{Verdict: "queued", NoticeFloor: floor}
	}
	verifyDeadline := time.Now().Add(verifyFor)
	if verifyDeadline.After(req.Deadline) {
		verifyDeadline = req.Deadline
	}
	result := CullDelivery{Verdict: "queued", NoticeFloor: floor}
	for time.Now().Before(verifyDeadline) {
		probeCtx, probeCancel := context.WithDeadline(context.Background(), verifyDeadline)
		receiptOut, receiptRC := outputCullCommand(probeCtx, env, receiptArgs...)
		noticeOut, noticeRC := outputCullCommand(probeCtx, env, "events", "--full", "--last", "20", "--from", req.SenderBase, "--thread", req.Thread)
		probeCancel()
		if noticeRC == 0 {
			for _, event := range decodeCullEvents(noticeOut) {
				if event.ID <= floor {
					continue
				}
				if isCullNotice(event, req) && event.ID > result.NoticeID {
					result.NoticeID = event.ID
				}
			}
		}
		if receiptRC == 0 && maxCullEventID(receiptOut) > receiptFloor {
			result.Verdict = "delivered"
			return result
		}
		if !sleepUntil(20*time.Millisecond, verifyDeadline) {
			break
		}
	}
	return result
}

// CullAckObserved recognizes either an on-thread ack from the target or an
// ack whose reply_to points at the notice. Requiring both would reject valid
// hcom clients that preserve only one correlation field.
func CullAckObserved(req CullRequest, delivery CullDelivery) (bool, int64) {
	if !time.Now().Before(req.Deadline) {
		return false, delivery.NoticeID
	}
	env := cullBusEnv(req.BusDir)
	ctx, cancel := context.WithDeadline(context.Background(), req.Deadline)
	defer cancel()

	noticeID := delivery.NoticeID
	if noticeID == 0 {
		out, rc := outputCullCommand(ctx, env, "events", "--full", "--last", "20", "--from", req.SenderBase, "--thread", req.Thread)
		if rc == 0 {
			for _, event := range decodeCullEvents(out) {
				if event.ID > delivery.NoticeFloor && isCullNotice(event, req) && event.ID > noticeID {
					noticeID = event.ID
				}
			}
		}
	}

	out, rc := outputCullCommand(ctx, env, "events", "--full", "--last", "100", "--from", req.TargetBase, "--intent", "ack")
	if rc != 0 {
		return false, noticeID
	}
	for _, event := range decodeCullEvents(out) {
		floor := delivery.NoticeFloor
		if noticeID > floor {
			floor = noticeID
		}
		if event.ID <= floor || event.Type != "message" || !cullWireNameMatches(event.Data.From, req.Target, req.TargetBase) || event.Data.Intent != "ack" {
			continue
		}
		onThread := event.Data.Thread == req.Thread
		replies := noticeID != 0 && (event.Data.ReplyTo == strconv.FormatInt(noticeID, 10) || event.Data.ReplyToLocal == noticeID)
		if onThread || replies {
			return true, noticeID
		}
	}
	return false, noticeID
}

func isCullNotice(event cullBusEvent, req CullRequest) bool {
	return event.Type == "message" && cullWireNameMatches(event.Data.From, req.Sender, req.SenderBase) && event.Data.Intent == "request" && event.Data.Thread == req.Thread
}

func cullWireNameMatches(got, full, base string) bool {
	return got != "" && (got == full || got == base)
}

func cullBusEnv(busDir string) []string {
	env := os.Environ()
	if busDir != "" && busDir != "null" {
		env = setEnv(env, "HCOM_DIR", busDir)
	}
	return env
}

func runCullCommand(ctx context.Context, env []string, args ...string) int {
	cmd := exec.CommandContext(ctx, "hcom", args...)
	cmd.Env = env
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}

func outputCullCommand(ctx context.Context, env []string, args ...string) ([]byte, int) {
	cmd := exec.CommandContext(ctx, "hcom", args...)
	cmd.Env = env
	cmd.WaitDelay = cullCommandWaitDelay
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), exitErr.ExitCode()
	}
	if err != nil {
		return stdout.Bytes(), 1
	}
	return stdout.Bytes(), 0
}

func decodeCullEvents(raw []byte) []cullBusEvent {
	var events []cullBusEvent
	if json.Unmarshal(raw, &events) == nil {
		return events
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var event cullBusEvent
		if json.Unmarshal(line, &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

func maxCullEventID(raw []byte) int64 {
	var max int64
	for _, event := range decodeCullEvents(raw) {
		if event.ID > max {
			max = event.ID
		}
	}
	return max
}

func lockSendWindowUntil(busDir, sender, target string, deadline time.Time) (func(), error) {
	if busDir == "" || busDir == "null" {
		busDir = ambientHcomDir()
	}
	sum := sha256.Sum256([]byte(busDir + "\x00" + sender + "\x00" + target))
	path := filepath.Join(os.TempDir(), fmt.Sprintf("herder-send-%x.lock", sum[:8]))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, err
	}
	for time.Now().Before(deadline) {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return func() {}, err
		}
		if !sleepUntil(20*time.Millisecond, deadline) {
			break
		}
	}
	_ = f.Close()
	return func() {}, fmt.Errorf("send window deadline expired")
}

func sleepUntil(interval time.Duration, deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	if interval > remaining {
		interval = remaining
	}
	time.Sleep(interval)
	return time.Now().Before(deadline)
}
