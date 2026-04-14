package mediaserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"

	"github.com/datahearth/streamline/internal/otelx"
)

// plexTVBaseURL is the canonical plex.tv API host. Tests pass an httptest URL
// instead via the unexported helpers; production paths in this package always
// pass plexTVBaseURL.
const plexTVBaseURL = "https://plex.tv/api/v2"

// plexAuthAppURL is the user-facing auth endpoint. Browser/popup opens this
// URL; the user logs into Plex there. Plex.tv then writes the auth token onto
// the pin record we created server-side.
const plexAuthAppURL = "https://app.plex.tv/auth"

// PlexPin is the result of starting a PIN-based OAuth flow. AuthURL is the
// URL the admin's browser must open (popup); ID is then polled until Plex
// fills in authToken.
type PlexPin struct {
	ID      uint64
	Code    string
	AuthURL string
	// ClientID is the X-Plex-Client-Identifier the flow used. Surfaced so a
	// read-only deploy operator can commit it to media_server.plex_client_id.
	ClientID string
}

// PlexPinResult is the polled state of a PIN. AuthToken is empty until the
// user completes the auth flow on app.plex.tv. Expired is true when Plex
// returns 404 or the recorded ExpiresAt has passed.
type PlexPinResult struct {
	AuthToken string
	Expired   bool
}

// PlexClientCreds carries the per-install identity Plex requires on every
// PIN request. Stored once-per-install in config (PlexClientID) and used as
// X-Plex-Client-Identifier; X-Plex-Product/Version identify Streamline.
type PlexClientCreds struct {
	ClientID string
	Product  string
	Version  string
}

// beginPlexPin posts to <baseURL>/pins?strong=true and returns the
// pin id+code plus the user-facing auth URL. baseURL is plex.tv in prod,
// httptest.URL in tests.
func beginPlexPin(
	ctx context.Context,
	baseURL string,
	creds PlexClientCreds,
) (PlexPin, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/pins?strong=true",
		nil,
	)
	if err != nil {
		return PlexPin{}, err
	}
	plexHeaders(req, creds, "")

	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return PlexPin{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return PlexPin{}, fmt.Errorf(
			"plex pin: unexpected status %d",
			resp.StatusCode,
		)
	}

	var raw struct {
		ID   uint64 `json:"id"`
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return PlexPin{}, fmt.Errorf("plex pin decode: %w", err)
	}

	return PlexPin{
		ID:      raw.ID,
		Code:    raw.Code,
		AuthURL: plexAuthURL(creds, raw.Code),
	}, nil
}

// pollPlexPin reads <baseURL>/pins/{id}. AuthToken is empty until the user
// completes the flow on app.plex.tv; 404 means the PIN expired and the
// caller should restart the flow.
func pollPlexPin(
	ctx context.Context,
	baseURL string,
	creds PlexClientCreds,
	pinID uint64,
) (PlexPinResult, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/pins/%d", baseURL, pinID),
		nil,
	)
	if err != nil {
		return PlexPinResult{}, err
	}
	plexHeaders(req, creds, "")

	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return PlexPinResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return PlexPinResult{Expired: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return PlexPinResult{}, fmt.Errorf(
			"plex pin poll: unexpected status %d",
			resp.StatusCode,
		)
	}

	var raw struct {
		AuthToken *string `json:"authToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return PlexPinResult{}, fmt.Errorf("plex pin poll decode: %w", err)
	}

	out := PlexPinResult{}
	if raw.AuthToken != nil {
		out.AuthToken = *raw.AuthToken
	}
	return out, nil
}

func plexHeaders(req *http.Request, creds PlexClientCreds, token string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", creds.Product)
	req.Header.Set("X-Plex-Version", creds.Version)
	req.Header.Set("X-Plex-Client-Identifier", creds.ClientID)
	req.Header.Set("X-Plex-Platform", runtime.GOOS)
	req.Header.Set("X-Plex-Device", creds.Product)
	req.Header.Set("X-Plex-Device-Name", creds.Product)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
}

func plexAuthURL(creds PlexClientCreds, pinCode string) string {
	q := url.Values{}
	q.Set("clientID", creds.ClientID)
	q.Set("code", pinCode)
	q.Set("context[device][product]", creds.Product)
	return plexAuthAppURL + "#?" + q.Encode()
}
