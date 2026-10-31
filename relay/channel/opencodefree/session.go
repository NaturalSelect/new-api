package opencodefree

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sessionTTL  = 30 * time.Minute
)

var sessions = newSessionRegistry()

// NOTE: The upstream only accepts "ses_" + 12 hex chars + 14 base62 chars, and the hex part must equal
// the high 48 bits of ~(timestamp*0x1000 + counter) because it re-derives it with its own id generator.
func newSessionID(timestampMS int64) string {
	value := uint64(timestampMS)*0x1000 + 1
	low48 := (^value) & 0xFFFF_FFFF_FFFF
	var timePart strings.Builder
	for i := 0; i < 6; i++ {
		timePart.WriteString(hex.EncodeToString([]byte{byte(low48 >> (40 - 8*i))}))
	}
	random := make([]byte, 14)
	_, _ = rand.Read(random)
	var randomPart strings.Builder
	for _, b := range random {
		randomPart.WriteByte(base62Chars[int(b)%len(base62Chars)])
	}
	return "ses_" + timePart.String() + randomPart.String()
}

type sessionEntry struct {
	id    string
	start time.Time
}

type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]sessionEntry
	now      func() time.Time
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{sessions: make(map[string]sessionEntry), now: time.Now}
}

func (r *sessionRegistry) get(holder string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if entry, ok := r.sessions[holder]; ok && now.Sub(entry.start) < sessionTTL {
		return entry.id
	}
	id := newSessionID(now.UnixMilli())
	r.sessions[holder] = sessionEntry{id: id, start: now}
	return id
}
