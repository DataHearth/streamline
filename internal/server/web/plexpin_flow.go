package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/auth"
)

// plexPinFlowTTL bounds how long a begun flow stays pollable. It sits above
// the SPA's 5-minute poll budget (web/app/lib/plex_pin.ts) and below the PIN
// lifetime plex.tv grants, so a PIN Plex itself expired surfaces as
// {"expired": true} rather than as a flow record that vanished mid-poll.
const plexPinFlowTTL = 10 * time.Minute

// plexPinFlow binds a plex.tv PIN to the session that started it. plex.tv
// scopes a PIN to the X-Plex-Client-Identifier that created it, but that
// identifier is per-install, so every caller of this instance polls under the
// same identity and upstream cannot tell them apart. The binding has to be
// ours.
type plexPinFlow struct {
	pinID     uint64
	userID    uint32
	jti       string
	expiresAt time.Time
}

// plexPinFlows maps opaque flow ids to in-flight PINs. Deliberately in-memory
// and per-process: a flow lives minutes and a restart just means the operator
// clicks Connect again. The app ships as a single binary over SQLite, so there
// is no second replica to share the map with.
type plexPinFlows struct {
	mu    sync.Mutex
	flows map[string]plexPinFlow
}

func newPlexPinFlows() *plexPinFlows {
	return &plexPinFlows{flows: make(map[string]plexPinFlow)}
}

// begin records pinID against the caller's identity and returns the opaque id
// the browser polls with. Under trusted-network auth every trusted caller
// carries a zero UserID and JTI; those callers are one principal by
// construction, so they legitimately share a flow.
func (p *plexPinFlows) begin(pinID uint64, c *auth.Claims) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	p.flows[id] = plexPinFlow{
		pinID:     pinID,
		userID:    c.UserID,
		jti:       c.JTI,
		expiresAt: time.Now().Add(plexPinFlowTTL),
	}
	return id, nil
}

// take resolves flowID to its PIN for the session that began it. Every miss
// (unknown, expired, or another session's) is reported identically so the
// caller cannot tell them apart — which is also why the sweep runs first
// rather than expiry being a branch of its own.
func (p *plexPinFlows) take(flowID string, c *auth.Claims) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	f, ok := p.flows[flowID]
	if !ok || f.userID != c.UserID || f.jti != c.JTI {
		return 0, false
	}
	return f.pinID, true
}

// consume drops a flow that reached a terminal state. The SPA polls every
// 1.5s, so a read must not consume: only a token or a Plex-side expiry ends
// the flow.
func (p *plexPinFlows) consume(flowID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.flows, flowID)
}

func (p *plexPinFlows) sweepLocked() {
	now := time.Now()
	for id, f := range p.flows {
		if !now.Before(f.expiresAt) {
			delete(p.flows, id)
		}
	}
}
