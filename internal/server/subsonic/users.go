package subsonic

import (
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/utils/random"
)

// webPasswordLen is the length of the throwaway login password an account
// created through Subsonic gets: the password the client sent is a Subsonic
// password, stored recoverable, so it never doubles as the web login. An
// admin sets a real one from the web UI when the account needs it.
const webPasswordLen = 32

// Subsonic permission flags an admin sets through createUser/updateUser. Each
// gates Subsonic calls only.
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

// accountErr maps the account service's refusals onto Subsonic errors; the
// message is the service's own, which names the rule.
func accountErr(err error) error {
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return errNotFoundErr
	case errors.Is(err, auth.ErrPasswordWeak):
		return &apiError{
			Code:    errGeneric,
			Message: "Password must be at least 8 characters",
		}
	case errors.Is(err, auth.ErrUserEmailExists):
		return &apiError{
			Code:    errGeneric,
			Message: "A user with this email already exists",
		}
	case errors.Is(err, auth.ErrLastAdmin):
		return &apiError{Code: errGeneric, Message: "Cannot remove the last admin"}
	case errors.Is(err, auth.ErrSelfDeleteForbidden):
		return &apiError{Code: errGeneric, Message: "Cannot delete yourself"}
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

func (h *Handler) requireAccounts(u *ent.User) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	if h.accounts == nil {
		return &apiError{
			Code:    errGeneric,
			Message: "User management is not available",
		}
	}
	return nil
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

// permissionPatch is the flags and bit rate a createUser or updateUser sends.
// def is the value of a flag the request leaves out: the API's defaults on
// create, the account's current value on update.
func permissionPatch(
	r *http.Request,
	def func(permission) bool,
) (map[permission]bool, *uint16, error) {
	flags := map[permission]bool{}
	for _, p := range []permission{
		permStream, permDownload, permPlaylists, permShare, permRate, permChangePassword,
	} {
		flags[p] = boolParam(r, string(p), def(p))
	}
	if r.FormValue("maxBitRate") == "" {
		return flags, nil, nil
	}
	kbps := intParam(r, "maxBitRate", -1, -1, 1<<20)
	if kbps < 0 || kbps > maxTranscodeKbps {
		return nil, nil, &apiError{
			Code:    errGeneric,
			Message: "maxBitRate must be between 0 and 320",
		}
	}
	if kbps > 0 {
		kbps = max(kbps, minTranscodeKbps)
	}
	n := uint16(kbps)
	return flags, &n, nil
}

func applyPermissions(
	up *ent.UserUpdateOne,
	flags map[permission]bool,
	kbps *uint16,
) {
	up.SetSubsonicCanStream(flags[permStream]).
		SetSubsonicCanDownload(flags[permDownload]).
		SetSubsonicCanManagePlaylists(flags[permPlaylists]).
		SetSubsonicCanShare(flags[permShare]).
		SetSubsonicCanRate(flags[permRate]).
		SetSubsonicCanChangePassword(flags[permChangePassword])
	if kbps != nil {
		up.SetSubsonicMaxBitRate(*kbps)
	}
}

// createUser makes a Streamline account through the account service, so the
// role write takes the same path as the admin API. The password sent is the
// account's Subsonic password; its web login password is random until an
// admin sets one. The username, when it is not the email, becomes the
// display name: the login is the email.
func (h *Handler) createUser(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	if err := h.requireAccounts(u); err != nil {
		return err
	}
	name := strings.TrimSpace(r.FormValue("username"))
	if name == "" {
		return missingParam("username")
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if email == "" {
		return missingParam("email")
	}
	if !strings.Contains(email, "@") {
		return &apiError{Code: errGeneric, Message: "email is not an address"}
	}
	password, err := passwordParam(r)
	if err != nil {
		return err
	}
	// The API's own defaults: settings and streaming on, the rest off.
	flags, kbps, err := permissionPatch(r, func(p permission) bool {
		return p == permStream || p == permChangePassword
	})
	if err != nil {
		return err
	}
	role := string(user.RoleMember)
	if boolParam(r, "adminRole", false) {
		role = string(user.RoleAdmin)
	}
	display := name
	if strings.EqualFold(name, email) {
		display = ""
	}

	created, err := h.accounts.CreateUserDirect(
		ctx, email, random.Alphanumeric(webPasswordLen), role, display,
	)
	if err != nil {
		return accountErr(err)
	}
	if err := h.accounts.SetSubsonicPassword(ctx, created.ID, password); err != nil {
		h.rollbackCreate(r, created.ID, u.ID)
		return accountErr(err)
	}
	up := h.ent.User.UpdateOneID(created.ID)
	applyPermissions(up, flags, kbps)
	if err := up.Exec(ctx); err != nil {
		h.rollbackCreate(r, created.ID, u.ID)
		return err
	}
	slog.InfoContext(ctx, "subsonic user created",
		"target.user.id", created.ID, "target.user.email", created.Email,
		"target.user.role", role)
	writeOK(w, r, nil)
	return nil
}

// rollbackCreate removes an account whose creation failed half way, so a
// retry is not refused as a duplicate.
func (h *Handler) rollbackCreate(r *http.Request, id, requesterID uint32) {
	if err := h.accounts.DeleteUser(r.Context(), id, requesterID); err != nil {
		slog.ErrorContext(r.Context(), "roll back subsonic user creation",
			"target.user.id", id, "error", err)
	}
}

// updateUser changes another account: email (the login), admin role,
// Subsonic password, permissions and bit rate cap, each only when sent.
func (h *Handler) updateUser(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	if err := h.requireAccounts(u); err != nil {
		return err
	}
	target, err := h.targetUser(r)
	if err != nil {
		return err
	}
	flags, kbps, err := permissionPatch(
		r,
		func(p permission) bool { return allowed(target, p) },
	)
	if err != nil {
		return err
	}
	var password string
	if r.FormValue("password") != "" {
		if password, err = passwordParam(r); err != nil {
			return err
		}
	}

	var patch auth.UserPatch
	if raw := strings.TrimSpace(r.FormValue("email")); raw != "" {
		email := strings.ToLower(raw)
		if !strings.Contains(email, "@") {
			return &apiError{Code: errGeneric, Message: "email is not an address"}
		}
		patch.Email = &email
	}
	if _, ok := r.Form["adminRole"]; ok {
		wantAdmin := boolParam(r, "adminRole", isAdmin(target))
		switch {
		case wantAdmin && !isAdmin(target):
			role := string(user.RoleAdmin)
			patch.Role = &role
		case !wantAdmin && isAdmin(target):
			role := string(user.RoleMember)
			patch.Role = &role
		}
	}
	if patch.Email != nil || patch.Role != nil {
		if err := h.accounts.UpdateUser(ctx, target.ID, patch); err != nil {
			return accountErr(err)
		}
	}
	if password != "" {
		if err := h.accounts.SetSubsonicPassword(
			ctx,
			target.ID,
			password,
		); err != nil {
			return accountErr(err)
		}
	}
	up := h.ent.User.UpdateOneID(target.ID)
	applyPermissions(up, flags, kbps)
	if err := up.Exec(ctx); err != nil {
		return lookupErr(err)
	}
	slog.InfoContext(ctx, "subsonic user updated", "target.user.id", target.ID)
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) deleteUser(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if err := h.requireAccounts(u); err != nil {
		return err
	}
	target, err := h.targetUser(r)
	if err != nil {
		return err
	}
	if err := h.accounts.DeleteUser(r.Context(), target.ID, u.ID); err != nil {
		return accountErr(err)
	}
	writeOK(w, r, nil)
	return nil
}

// changePassword sets a Subsonic password: the caller's own when they may
// change it, anyone's for an admin. The web login password is not this API's.
func (h *Handler) changePassword(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	target, err := h.targetUser(r)
	if err != nil {
		if !isAdmin(u) {
			return errNotAuthorizedErr
		}
		return err
	}
	switch {
	case isAdmin(u):
	case target.ID != u.ID:
		return errNotAuthorizedErr
	default:
		if err := require(u, permChangePassword); err != nil {
			return err
		}
	}
	if h.accounts == nil {
		return &apiError{
			Code:    errGeneric,
			Message: "User management is not available",
		}
	}
	password, err := passwordParam(r)
	if err != nil {
		return err
	}
	if err := h.accounts.SetSubsonicPassword(
		r.Context(),
		target.ID,
		password,
	); err != nil {
		return accountErr(err)
	}
	writeOK(w, r, nil)
	return nil
}
