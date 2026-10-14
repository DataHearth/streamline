package subsonic

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
)

// Every id this server hands out is "<kind>-<row id>", so one parameter can
// name any kind and a handler can refuse the wrong one.
const (
	kindArtist   = "ar"
	kindAlbum    = "al"
	kindTrack    = "tr"
	kindPlaylist = "pl"
	kindShare    = "sh"
	kindRadio    = "rs"
)

var knownKinds = map[string]struct{}{
	kindArtist: {}, kindAlbum: {}, kindTrack: {},
	kindPlaylist: {}, kindShare: {}, kindRadio: {},
}

func formatID(kind string, id uint32) string {
	return fmt.Sprintf("%s-%d", kind, id)
}

func artistID(id uint32) string   { return formatID(kindArtist, id) }
func albumID(id uint32) string    { return formatID(kindAlbum, id) }
func trackID(id uint32) string    { return formatID(kindTrack, id) }
func playlistID(id uint32) string { return formatID(kindPlaylist, id) }
func shareID(id uint32) string    { return formatID(kindShare, id) }
func radioID(id uint32) string    { return formatID(kindRadio, id) }

func parseID(s string) (string, uint32, error) {
	kind, tail, ok := strings.Cut(s, "-")
	if _, known := knownKinds[kind]; !ok || !known {
		return "", 0, fmt.Errorf("malformed id %q", s)
	}
	n, err := strconv.ParseUint(tail, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("malformed id %q: %w", s, err)
	}
	return kind, uint32(n), nil
}

func missingParam(name string) error {
	return &apiError{
		Code:    errMissingParam,
		Message: "Required parameter is missing: " + name,
	}
}

var errNotFoundErr = &apiError{
	Code:    errNotFound,
	Message: "The requested data was not found",
}

var errNotAuthorizedErr = &apiError{
	Code:    errNotAuthorized,
	Message: "User is not authorized for the given operation",
}

func lookupErr(err error) error {
	if ent.IsNotFound(err) {
		return errNotFoundErr
	}
	return err
}

// anyIDParam reads the named parameter whatever kind it holds.
func anyIDParamNamed(r *http.Request, name string) (string, uint32, error) {
	raw := r.FormValue(name)
	if raw == "" {
		return "", 0, missingParam(name)
	}
	kind, id, err := parseID(raw)
	if err != nil {
		return "", 0, errNotFoundErr
	}
	return kind, id, nil
}

// anyIDParam reads the id parameter whatever kind it names.
func anyIDParam(r *http.Request) (string, uint32, error) {
	return anyIDParamNamed(r, "id")
}

func idParamNamed(r *http.Request, name, wantKind string) (uint32, error) {
	kind, id, err := anyIDParamNamed(r, name)
	if err != nil {
		return 0, err
	}
	if kind != wantKind {
		return 0, errNotFoundErr
	}
	return id, nil
}

func idParam(r *http.Request, wantKind string) (uint32, error) {
	return idParamNamed(r, "id", wantKind)
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	n, err := strconv.Atoi(r.FormValue(name))
	if err != nil {
		return def
	}
	return min(max(n, lo), hi)
}

func parseMillis(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

// boolParam reads a Subsonic boolean; absent or unreadable is def.
func boolParam(r *http.Request, name string, def bool) bool {
	b, err := strconv.ParseBool(r.FormValue(name))
	if err != nil {
		return def
	}
	return b
}

// millisParam reads an epoch-milliseconds parameter. present reports whether
// it was sent at all; a value that is not a number is a missing one.
func millisParam(r *http.Request, name string) (t time.Time, present bool) {
	raw := r.FormValue(name)
	if raw == "" {
		return time.Time{}, false
	}
	ms, err := parseMillis(raw)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}
