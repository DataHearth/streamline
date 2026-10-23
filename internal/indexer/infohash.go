package indexer

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strings"
)

// infoHash returns a release's v1 info hash as lowercase hex — the form every
// download client reports and a download record stores — taken from the
// indexer's own hash field or else from the first magnet link carrying one.
// Empty when none does: a .torrent link names no hash until it is fetched.
func infoHash(hash string, links ...string) string {
	if h := normalizeInfoHash(hash); h != "" {
		return h
	}
	for _, link := range links {
		if h := magnetInfoHash(link); h != "" {
			return h
		}
	}
	return ""
}

func magnetInfoHash(link string) string {
	if !strings.HasPrefix(link, "magnet:") {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		if btih, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			return normalizeInfoHash(btih)
		}
	}
	return ""
}

// normalizeInfoHash accepts the 32-character base32 spelling as well, which
// older magnets still use and which no client ever reports back.
func normalizeInfoHash(h string) string {
	switch len(h) {
	case hex.EncodedLen(20):
		if _, err := hex.DecodeString(h); err == nil {
			return strings.ToLower(h)
		}
	case base32.StdEncoding.EncodedLen(20):
		if b, err := base32.StdEncoding.DecodeString(
			strings.ToUpper(h),
		); err == nil {
			return hex.EncodeToString(b)
		}
	}
	return ""
}
