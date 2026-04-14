package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// JTILogValue returns a session.id_hash value that identifies a session in
// logs without leaking the raw jti (which, combined with the signing secret,
// is the session bearer credential). The 16-char prefix of the SHA-256
// hex digest is enough to correlate log lines while being computationally
// infeasible to reverse.
func JTILogValue(jti string) string {
	sum := sha256.Sum256([]byte(jti))
	return hex.EncodeToString(sum[:])[:16]
}
