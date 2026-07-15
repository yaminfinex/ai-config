package cullcmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/send"
)

const (
	defaultGraceTimeoutMS = 120000
	gracePollInterval     = 250 * time.Millisecond
	releaseNotice         = "Before this session is released, release external resources you own, then acknowledge this request. Herder will proceed after a bounded grace window."
)

func gracefulRelease(rec registry.Record, pane, term string, opts options, stdout io.Writer) {
	if opts.now || opts.goneOnly || pane == "" || term == "" || rec.HcomName == "" || rec.HcomName == "null" {
		return
	}
	if rec.HcomVerified != nil && !*rec.HcomVerified {
		fmt.Fprintln(stdout, "release notice: skipped (target bus identity is unverified); proceeding")
		return
	}

	deadline := time.Now().Add(time.Duration(opts.graceTimeoutMS) * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	sender := hcomidentity.ResolveLiveContext(ctx, rec.HcomDir, hcomidentity.CurrentEvidence(os.Getenv("HERDR_PANE_ID")))
	cancel()
	if !sender.Verified {
		fmt.Fprintf(stdout, "release notice: skipped (caller bus identity unverified: %s); proceeding\n", sender.Reason)
		return
	}

	request := send.CullRequest{
		Sender:   sender.Name,
		Target:   rec.HcomName,
		BusDir:   rec.HcomDir,
		Thread:   newCullThread(),
		Message:  releaseNotice,
		Deadline: deadline,
	}
	delivery := send.DeliverCullRequest(request)
	switch delivery.Verdict {
	case "delivered":
		fmt.Fprintln(stdout, "release notice: verify=delivered; waiting for acknowledgement or working->idle")
	case "queued":
		fmt.Fprintln(stdout, "release notice: verify=queued; waiting for acknowledgement or working->idle")
	case "not_joined":
		fmt.Fprintln(stdout, "release notice: target is not joined; proceeding")
		return
	default:
		fmt.Fprintln(stdout, "release notice: delivery failed; proceeding")
		return
	}

	sawWorking := false
	for time.Now().Before(deadline) {
		acked, noticeID := send.CullAckObserved(request, delivery)
		delivery.NoticeID = noticeID
		if acked {
			fmt.Fprintln(stdout, "release notice: acknowledged; proceeding")
			return
		}
		status, live := liveStatusForTerminal(term, deadline)
		if !live {
			fmt.Fprintln(stdout, "release notice: target no longer live; proceeding")
			return
		}
		if status == "working" {
			sawWorking = true
		} else if sawWorking && status == "idle" {
			fmt.Fprintln(stdout, "release notice: observed working->idle; proceeding")
			return
		}
		sleepGrace(deadline)
	}
	fmt.Fprintf(stdout, "release notice: grace window expired after %dms; proceeding\n", opts.graceTimeoutMS)
}

func liveStatusForTerminal(term string, deadline time.Time) (string, bool) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "herdr", "agent", "list")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	agents, err := herdrcli.ParseAgentList(out)
	if err != nil {
		return "", false
	}
	for _, agent := range agents {
		if agent.TerminalID != nil && *agent.TerminalID == term {
			return agent.Status, true
		}
	}
	return "", false
}

func newCullThread() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("herder-cull-%d", time.Now().UnixNano())
	}
	return "herder-cull-" + hex.EncodeToString(raw[:])
}

func sleepGrace(deadline time.Time) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return
	}
	if remaining > gracePollInterval {
		remaining = gracePollInterval
	}
	time.Sleep(remaining)
}
