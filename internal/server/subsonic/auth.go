package subsonic

import (
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

const wrongCredentialsMsg = "Wrong username or password"

// checkCredentials validates Subsonic auth params against the user's stored
// subsonic password: token form t=md5(password+salt), or legacy p= (plain or
// "enc:"+hex). An empty stored password disables Subsonic access entirely.
func checkCredentials(stored string, params url.Values) bool {
	if stored == "" {
		return false
	}
	if t := params.Get("t"); t != "" {
		sum := md5.Sum([]byte(stored + params.Get("s"))) //nolint:gosec // see import
		return subtle.ConstantTimeCompare(
			[]byte(strings.ToLower(t)),
			[]byte(hex.EncodeToString(sum[:])),
		) == 1
	}
	p := params.Get("p")
	if hexed, ok := strings.CutPrefix(p, "enc:"); ok {
		decoded, err := hex.DecodeString(hexed)
		if err != nil {
			return false
		}
		p = string(decoded)
	}
	return p != "" && subtle.ConstantTimeCompare([]byte(p), []byte(stored)) == 1
}

// authenticate resolves the caller from the u parameter, which carries the
// login email. An unknown user and bad credentials answer the same error so
// the response does not reveal which emails exist.
func (h *Handler) authenticate(r *http.Request) (*ent.User, error) {
	login := r.URL.Query().Get("u")
	if login == "" {
		return nil, &apiError{
			Code:    errMissingParam,
			Message: "Required parameter is missing: u",
		}
	}
	u, err := h.ent.User.Query().Where(user.EmailEQ(login)).Only(r.Context())
	if ent.IsNotFound(err) {
		return nil, &apiError{
			Code:    errWrongCredentials,
			Message: wrongCredentialsMsg,
		}
	}
	if err != nil {
		return nil, &apiError{Code: errGeneric, Message: "Internal error"}
	}
	if !checkCredentials(u.SubsonicPassword, r.URL.Query()) {
		return nil, &apiError{
			Code:    errWrongCredentials,
			Message: wrongCredentialsMsg,
		}
	}
	return u, nil
}
