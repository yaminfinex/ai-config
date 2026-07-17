package liveness

import (
	"errors"
	"syscall"
	"time"
)

const DefaultKeepaliveStarvation = 5 * time.Minute

func ProbePID(pid int) Signal {
	if pid <= 0 {
		return Signal{State: StateUnknown, ObservedVia: "pid_probe"}
	}
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return Signal{State: StateAlive, ObservedVia: "pid_probe"}
	case errors.Is(err, syscall.ESRCH):
		return Signal{State: StateDead, ObservedVia: "pid_probe"}
	default:
		return Signal{State: StateUnknown, ObservedVia: "pid_probe"}
	}
}

func KeepaliveFromAge(ageSeconds int64) KeepaliveState {
	if ageSeconds < 0 {
		return KeepaliveUnknown
	}
	if time.Duration(ageSeconds)*time.Second > DefaultKeepaliveStarvation {
		return KeepaliveStarved
	}
	return KeepaliveFresh
}
