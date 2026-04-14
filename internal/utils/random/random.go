// Package random holds crypto/rand wrappers.
package random

import (
	"crypto/rand"
	"encoding/base64"
)

// Must returns a URL-safe base64 string (no padding) encoding n bytes from
// crypto/rand. Panics on rand read failure (bubbles up to the top-level
// recoverer). Used for OIDC state/nonce/PKCE-verifier values where padding
// would break URL embedding.
func Must(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("random.Must: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
