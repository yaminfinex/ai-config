package agentstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// NewID returns a UUIDv7: 48-bit unix milliseconds then random bits, so ids
// sort by time and never collide across processes.
func NewID(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		panic("agentstore: crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return format(b)
}

// DerivedID is the deterministic id for imported records: sha256 of the
// source bytes folded into a UUID-shaped string (version nibble 8 marks it
// as derived, never colliding with NewID's version 7).
func DerivedID(source []byte) string {
	sum := sha256.Sum256(source)
	var b [16]byte
	copy(b[:], sum[:16])
	b[6] = (b[6] & 0x0f) | 0x80
	b[8] = (b[8] & 0x3f) | 0x80
	return format(b)
}

// ValidID reports whether id has the UUID shape every event id must carry.
func ValidID(id string) bool { return idPattern.MatchString(id) }

func format(b [16]byte) string {
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
