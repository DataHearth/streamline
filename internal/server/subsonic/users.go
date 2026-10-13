package subsonic

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/auth"
)

// Subsonic permission flags, set by an admin from the web UI's user page.
// Each gates Subsonic calls only.
type permission string

const (
	permStream         permission = "streamRole"
	permDownload       permission = "downloadRole"
	permPlaylists      permission = "playlistRole"
	permShare          permission = "shareRole"
	permRate           permission = "commentRole"
	permChangePassword permission = "settingsRole"
)

func allowed(u *ent.User, p permission) bool {
	switch p {
	case permStream:
		return u.SubsonicCanStream
	case permDownload:
		return u.SubsonicCanDownload
	case permPlaylists:
		return u.SubsonicCanManagePlaylists
	case permShare:
		return u.SubsonicCanShare
	case permRate:
		return u.SubsonicCanRate
	case permChangePassword:
		return u.SubsonicCanChangePassword
	}
	return false
}

// require refuses the call with error 50 unless u holds p.
func require(u *ent.User, p permission) error {
	if !allowed(u, p) {
		return errNotAuthorizedErr
	}
	return nil
}

// toSubsonicUser renders an account. Jukebox, podcasts, uploads, cover art
// edits and video conversion do not exist here, so their roles are false.
func toSubsonicUser(u *ent.User) *subsonicUser {
	return &subsonicUser{
		Username:          u.Email,
		Email:             u.Email,
		ScrobblingEnabled: true,
		MaxBitRate:        u.SubsonicMaxBitRate,
		AdminRole:         isAdmin(u),
		SettingsRole:      u.SubsonicCanChangePassword,
		DownloadRole:      u.SubsonicCanDownload,
		PlaylistRole:      u.SubsonicCanManagePlaylists,
		CommentRole:       u.SubsonicCanRate,
		StreamRole:        u.SubsonicCanStream,
		ShareRole:         u.SubsonicCanShare,
		Folder:            []int{1},
	}
}

// accountErr maps the account service's refusals onto Subsonic errors.
func accountErr(err error) error {
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return errNotFoundErr
	case errors.Is(err, auth.ErrPasswordWeak):
		return &apiError{
			Code:    errGeneric,
			Message: "Password must be at least 8 characters",
		}
	}
	return err
}

// passwordParam reads a password the way the API sends it: plain, or
// "enc:" followed by its hex.
func passwordParam(r *http.Request) (string, error) {
	p := r.FormValue("password")
	if p == "" {
		return "", missingParam("password")
	}
	if hexed, ok := strings.CutPrefix(p, "enc:"); ok {
		decoded, err := hex.DecodeString(hexed)
		if err != nil {
			return "", &apiError{Code: errGeneric, Message: "Malformed password"}
		}
		p = string(decoded)
	}
	return p, nil
}

// targetUser loads the account the username parameter names. The Subsonic
// user name is the login email.
func (h *Handler) targetUser(r *http.Request) (*ent.User, error) {
	name := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	if name == "" {
		return nil, missingParam("username")
	}
	u, err := h.ent.User.Query().Where(user.EmailEQ(name)).Only(r.Context())
	return u, lookupErr(err)
}

// getUser describes the caller's own account, or any account to an admin.
func (h *Handler) getUser(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	target, err := h.targetUser(r)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Code == errNotFound && !isAdmin(u) {
			// Whether an account exists is not a member's business.
			return errNotAuthorizedErr
		}
		return err
	}
	if target.ID != u.ID && !isAdmin(u) {
		return errNotAuthorizedErr
	}
	writeOK(w, r, &body{User: toSubsonicUser(target)})
	return nil
}

func (h *Handler) getUsers(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	us, err := h.ent.User.Query().Order(user.ByEmail()).All(r.Context())
	if err != nil {
		return err
	}
	out := &subsonicUsers{User: make([]subsonicUser, 0, len(us))}
	for _, x := range us {
		out.User = append(out.User, *toSubsonicUser(x))
	}
	writeOK(w, r, &body{Users: out})
	return nil
}

// managedInStreamline answers the account-changing calls: accounts are
// managed by an admin in Streamline's web UI, never through a Subsonic
// credential, which is stored recoverable and travels in every request.
func (h *Handler) managedInStreamline(
	_ http.ResponseWriter,
	_ *http.Request,
	_ *ent.User,
) error {
	return &apiError{
		Code:    errNotAuthorized,
		Message: "Users are managed in Streamline's web UI",
	}
}

// changePassword sets the caller's own Subsonic password, when their account
// may. Nobody changes another account's through this API, admins included;
// the web login password is never this API's.
func (h *Handler) changePassword(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	name := strings.TrimSpace(r.FormValue("username"))
	if name == "" {
		return missingParam("username")
	}
	if !strings.EqualFold(name, u.Email) {
		return errNotAuthorizedErr
	}
	if err := require(u, permChangePassword); err != nil {
		return err
	}
	if h.accounts == nil {
		return &apiError{
			Code:    errGeneric,
			Message: "Password changes are not available",
		}
	}
	password, err := passwordParam(r)
	if err != nil {
		return err
	}
	if err := h.accounts.SetSubsonicPassword(
		r.Context(),
		u.ID,
		password,
	); err != nil {
		return accountErr(err)
	}
	writeOK(w, r, nil)
	return nil
}
