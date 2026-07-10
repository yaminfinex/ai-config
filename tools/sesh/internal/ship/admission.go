package ship

import (
	"context"
	"time"
)

// hintAdmission coalesces daemon wakeups without changing what a pass does.
// Filesystem hints may be immediate after idle, while sustained hints remain
// bounded start-to-start. Periodic and retry admissions share the same one-
// pass pending state so races cannot build a catch-up queue.
type hintAdmission struct {
	interval time.Duration

	lastHintStart   time.Time
	hintPending     bool
	hintDue         time.Time
	periodicPending bool
	holdUntil       time.Time
}

func newHintAdmission(interval time.Duration) *hintAdmission {
	return &hintAdmission{interval: interval}
}

func (a *hintAdmission) Hint(now time.Time) {
	if a.hintPending {
		return
	}
	a.hintPending = true
	a.hintDue = now
	if earliest := a.lastHintStart.Add(a.interval); !a.lastHintStart.IsZero() && now.Before(earliest) {
		a.hintDue = earliest
	}
}

func (a *hintAdmission) Periodic() {
	a.periodicPending = true
}

func (a *hintAdmission) HoldUntil(deadline time.Time) {
	if deadline.After(a.holdUntil) {
		a.holdUntil = deadline
	}
}

func (a *hintAdmission) Next(now time.Time) (time.Time, bool) {
	var deadline time.Time
	switch {
	case a.periodicPending:
		deadline = now
	case a.hintPending:
		deadline = a.hintDue
	case !a.holdUntil.IsZero():
		deadline = a.holdUntil
	default:
		return time.Time{}, false
	}
	if deadline.Before(a.holdUntil) {
		deadline = a.holdUntil
	}
	return deadline, true
}

func (a *hintAdmission) Take(now time.Time) bool {
	deadline, ok := a.Next(now)
	if !ok || now.Before(deadline) {
		return false
	}
	servedHint := a.hintPending
	a.hintPending = false
	a.periodicPending = false
	a.holdUntil = time.Time{}
	if servedHint {
		a.lastHintStart = now
	}
	return true
}

func waitForAdmission(
	ctx context.Context,
	periodic <-chan time.Time,
	hints <-chan struct{},
	a *hintAdmission,
	onPeriodic func(),
) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-periodic:
			a.Periodic()
			if onPeriodic != nil {
				onPeriodic()
			}
		default:
		}
		select {
		case <-hints:
			a.Hint(time.Now())
		default:
		}
		now := time.Now()
		deadline, scheduled := a.Next(now)
		if scheduled && !deadline.After(now) {
			if a.Take(now) {
				return nil
			}
			continue
		}

		var timer *time.Timer
		var timerC <-chan time.Time
		if scheduled {
			timer = time.NewTimer(time.Until(deadline))
			timerC = timer.C
		}

		select {
		case <-ctx.Done():
			stopTimer(timer)
			return ctx.Err()
		case <-periodic:
			stopTimer(timer)
			a.Periodic()
			if onPeriodic != nil {
				onPeriodic()
			}
		case <-hints:
			stopTimer(timer)
			a.Hint(time.Now())
		case <-timerC:
			// Loop once so a simultaneously-ready periodic tick or hint is
			// absorbed before the admission is taken.
		}
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
