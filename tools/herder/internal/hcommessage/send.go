// Package hcommessage sends attributed web requests through hcom's supported
// send command.
package hcommessage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ai-config/tools/herder/internal/hcomcli"
)

const sendTimeout = 10 * time.Second

// SendRequest always supplies intent=request; callers cannot choose a weaker
// intent through this API.
func SendRequest(ctx context.Context, target, sender, message string) error {
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	cmd := hcomcli.CommandContext(sendCtx, "send", "@"+target, "--intent", "request", "--from", sender, "--", message)
	if _, err := cmd.Output(); err != nil {
		if sendCtx.Err() != nil {
			return fmt.Errorf("hcom send timed out: %w", sendCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(bytes.TrimSpace(exitErr.Stderr)) > 0 {
			return errors.New(string(bytes.TrimSpace(exitErr.Stderr)))
		}
		return errors.New(strings.TrimSpace(err.Error()))
	}
	return nil
}
