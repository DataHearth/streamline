package restapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

// releaseHandlePrefix marks a download_url this server sealed. Anything
// without it is taken as a plain link, which checkReleaseSource still holds
// to the configured indexers — so an API client posting a link of its own is
// unaffected.
const releaseHandlePrefix = "slr1."

var errBadReleaseHandle = errors.New("release handle does not open")

// sealReleaseLink turns an indexer's download link into an opaque handle.
//
// The link authenticates the download: Jackett puts jackett_apikey on it,
// Prowlarr apikey, and a torznab indexer's host is often an internal address.
// Search and browse are member endpoints, so returning it verbatim handed
// every member the indexer's key. The handle is encrypted rather than signed,
// since a signature would still carry the link in the clear; the member posts
// it back to grab, and only this server can open it. The key comes from the
// session secret, so rotating that also retires every outstanding handle — a
// grab from a stale page then needs a fresh search.
func sealReleaseLink(link string) string {
	aead, err := releaseAEAD()
	if err != nil {
		return ""
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	sealed := aead.Seal(nonce, nonce, []byte(link), nil)
	return releaseHandlePrefix + base64.RawURLEncoding.EncodeToString(sealed)
}

// openReleaseLink reverses sealReleaseLink, and passes a plain link through.
func openReleaseLink(v string) (string, error) {
	enc, sealed := strings.CutPrefix(v, releaseHandlePrefix)
	if !sealed {
		return v, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", errBadReleaseHandle
	}
	aead, err := releaseAEAD()
	if err != nil {
		return "", err
	}
	n := aead.NonceSize()
	if len(raw) < n {
		return "", errBadReleaseHandle
	}
	link, err := aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errBadReleaseHandle
	}
	return string(link), nil
}

func releaseAEAD() (cipher.AEAD, error) {
	a := config.Get().Auth
	secret := config.SecretValue(a.SessionSecret, a.SessionSecretFile)
	// Derived, not the secret itself: the session secret also signs JWTs, and
	// one key serving two primitives is a key neither can reason about.
	key := sha256.Sum256([]byte("streamline release handle\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
