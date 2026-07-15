package send

import (
	"fmt"

	"ai-config/tools/herder/internal/hcomidentity"
)

// SenderIdentityRefusal is the fail-closed contract for a bus send whose
// caller cannot prove one joined hcom identity. Cause states the failed proof;
// Remedy states how the operator can repair it before retrying.
type SenderIdentityRefusal struct {
	Cause  string
	Remedy string
}

func (e *SenderIdentityRefusal) Error() string {
	return fmt.Sprintf("%s. %s", e.Cause, e.Remedy)
}

// ResolveLiveSender proves the caller's current joined hcom row from the
// evidence classes hcomidentity owns: session id, process id, or pane id.
func ResolveLiveSender(busDir string, evidence hcomidentity.Evidence) (string, error) {
	rows, err := hcomidentity.List(busDir)
	if err != nil {
		return "", &SenderIdentityRefusal{
			Cause:  "the live hcom roster is unavailable: " + err.Error(),
			Remedy: "Restore access to this session's hcom bus, then retry; enrolling cannot repair an unavailable roster",
		}
	}
	resolved := hcomidentity.Resolve(rows, evidence)
	if !resolved.Verified {
		cause := resolved.Reason
		if cause == "" {
			cause = "no joined hcom row is verified for the calling session"
		}
		return "", &SenderIdentityRefusal{
			Cause:  cause,
			Remedy: "Run `herder enroll` from this session to repair its bus binding, then retry",
		}
	}
	return resolved.Name, nil
}

// VerifyStoredSender applies the compact --then identity-honesty precedent:
// the caller's stored registry name must equal the one live row proven by the
// hcomidentity evidence interface. Registry provenance internals are not read.
func VerifyStoredSender(storedName, busDir string, evidence hcomidentity.Evidence) (string, error) {
	if storedName == "" || storedName == "null" {
		return "", &SenderIdentityRefusal{
			Cause:  "the calling session's registry row has no bus name",
			Remedy: "Run `herder enroll` from this session to establish a verified bus binding, then retry",
		}
	}
	live, err := ResolveLiveSender(busDir, evidence)
	if err != nil {
		return "", err
	}
	if live != storedName {
		return "", &SenderIdentityRefusal{
			Cause:  fmt.Sprintf("the registry records @%s but live caller evidence proves @%s", storedName, live),
			Remedy: "Run `herder enroll` from this session to repair the stale bus binding, then retry",
		}
	}
	return live, nil
}
