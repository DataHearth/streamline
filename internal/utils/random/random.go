// Package random holds crypto/rand wrappers.
package random

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
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

const alphanumeric = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Alphanumeric returns n characters drawn uniformly from [a-zA-Z0-9]. Panics
// on rand read failure, like Must.
func Alphanumeric(n int) string {
	limit := big.NewInt(int64(len(alphanumeric)))
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, limit)
		if err != nil {
			panic("random.Alphanumeric: " + err.Error())
		}
		b[i] = alphanumeric[idx.Int64()]
	}
	return string(b)
}
