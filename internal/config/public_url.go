package config

import (
	"os"
	"strconv"
)

// PublicURL returns the public base URL used for OIDC redirect URIs, invite
// links and admin-visible display. Priority: STREAMLINE_PUBLIC_URL env var
// (not part of the config schema — read directly) then http://host:port from
// the config. Should be overridden to an https URL when running behind a
// reverse proxy.
func PublicURL() string {
	if u := os.Getenv("STREAMLINE_PUBLIC_URL"); u != "" {
		return u
	}
	c := Get()
	return "http://" + c.Server.Host + ":" + strconv.Itoa(int(c.Server.Port))
}
