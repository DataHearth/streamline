package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrSecretRequired refuses an edit that moves a media server, download
	// client or indexer to another address while keeping its stored secret.
	// A blank secret in a patch means "keep the stored one", so without this a
	// typo in the host, or a stale address, handed the token, password or API
	// key to a machine it was never meant for on the next call.
	ErrSecretRequired = errors.New(
		"the address changed: enter the secret again to send it to the new address",
	)

	// ErrSecretFileDestination refuses the same move for a secret read from a
	// *_file path. The operator owns that secret, and the admin moving the
	// address may not even be able to read it; re-pointing it is the file's
	// call, not the API's.
	ErrSecretFileDestination = errors.New(
		"the secret is file-managed; change the address in the config file, not the UI",
	)
)

// urlDestination reduces a base URL to where a request is sent: lower-cased
// scheme and host, and the port with the scheme's default filled in. A path,
// a trailing slash or a letter case change is not a move. A value that does
// not parse as an absolute URL is compared as written.
func urlDestination(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimRight(raw, "/"))
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// hostDestination is urlDestination for an entry that stores host, port and
// TLS apart, the way buildBaseURL in the download and indexer packages
// assembles them.
func hostDestination(host string, port uint16, useSSL bool) string {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(
		strings.ToLower(strings.TrimSpace(host)),
		strconv.Itoa(int(port)),
	)
}

// guardMovedSecret decides what happens to one stored secret when its entry
// moves to another destination. A secret the patch replaces goes with it; a
// file-managed one refuses the move; an inline one the entry sends refuses it
// until it is entered again; an inline one the entry does not send (a
// qBittorrent password under api_key auth) is dropped, since a later switch of
// auth method would otherwise send it to the new address.
func guardMovedSecret(
	inline *string,
	file string,
	supplied *string,
	inUse bool,
) error {
	if supplied != nil && *supplied != "" {
		return nil
	}
	if file != "" {
		return ErrSecretFileDestination
	}
	if *inline == "" {
		return nil
	}
	if inUse {
		return ErrSecretRequired
	}
	*inline = ""
	return nil
}
