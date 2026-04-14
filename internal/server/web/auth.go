package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/utils/httputil"
	"github.com/datahearth/streamline/internal/utils/random"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
)

const (
	defaultSessionTTL = 168 * time.Hour
	unknownUserAgent  = "unknown"
	defaultLandingURL = "/dashboard"
)

// registerWebAuthRoutes wires the JSON auth endpoints and the OIDC redirect
// dance. The SPA owns all login/register UI; this layer just shuffles
// credentials and cookies.
//
// CSRF posture: POST endpoints rely on SameSite=Lax cookies for CSRF
// mitigation (see internal/auth/cookie.go). Adequate for a self-hosted
// personal media manager; revisit if Streamline is ever deployed behind a
// shared subdomain structure.
func (h *Handler) registerWebAuthRoutes(r chi.Router) {
	r.Get("/auth/config", h.authConfig)
	r.Get("/auth/invite/{token}", h.authInvite)
	r.Post("/auth/login", h.authLogin)
	r.Post("/auth/register", h.authRegister)
	r.Post("/auth/logout", h.authLogout)
	r.Get("/auth/oidc/{name}/start", h.oidcStart)
	r.Get("/auth/oidc/{name}/callback", h.oidcCallback)
}

type authConfigResponse struct {
	RegistrationMode string         `json:"registration_mode"`
	Providers        []providerInfo `json:"providers"`
	ReadOnly         bool           `json:"read_only"`
}

type providerInfo struct {
	Name string `json:"name"`
}

func (h *Handler) authConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	body := authConfigResponse{
		RegistrationMode: cfg.Auth.RegistrationMode,
		Providers:        make([]providerInfo, 0, len(cfg.Auth.OIDC)),
		ReadOnly:         cfg.ReadOnly,
	}
	for _, p := range cfg.Auth.OIDC {
		body.Providers = append(body.Providers, providerInfo{Name: p.Name})
	}
	writeJSON(w, r, http.StatusOK, body)
}

type invitePrefillResponse struct {
	Email       string `json:"email,omitempty"`
	EmailLocked bool   `json:"email_locked"`
}

func (h *Handler) authInvite(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	inv, err := h.auth.LookupInviteForPrefill(r.Context(), tok)
	if err != nil {
		writeError(
			w,
			r,
			http.StatusBadRequest,
			"Invite invalid or expired",
			"invite_invalid",
		)
		return
	}
	body := invitePrefillResponse{}
	if inv.Email != "" {
		body.Email = inv.Email
		body.EmailLocked = true
	}
	writeJSON(w, r, http.StatusOK, body)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) authLogin(w http.ResponseWriter, r *http.Request) {
	if !h.allowAttempt(w, r) {
		return
	}
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(
			w,
			r,
			http.StatusBadRequest,
			"Invalid request body",
			"bad_request",
		)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	tok, err := h.auth.Login(r.Context(), email, body.Password, sessionMeta(r))
	if err != nil {
		msg := "Invalid credentials"
		if locked, ok := errors.AsType[auth.ErrAccountLockedT](err); ok {
			msg = lockedMessage(locked.LockedUntil)
		}
		writeError(w, r, http.StatusUnauthorized, msg, "invalid_credentials")
		return
	}
	auth.SetSession(w, r, tok, h.sessionTTL())
	w.WriteHeader(http.StatusNoContent)
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Confirm     string `json:"confirm"`
	DisplayName string `json:"display_name"`
	Token       string `json:"token"`
}

func (h *Handler) authRegister(w http.ResponseWriter, r *http.Request) {
	if !h.allowAttempt(w, r) {
		return
	}
	var body registerRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(
			w,
			r,
			http.StatusBadRequest,
			"Invalid request body",
			"bad_request",
		)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if len(body.Password) < 8 {
		writeError(
			w,
			r,
			http.StatusBadRequest,
			"Password must be at least 8 characters",
			"weak_password",
		)
		return
	}
	if body.Password != body.Confirm {
		writeError(
			w,
			r,
			http.StatusBadRequest,
			"Passwords do not match",
			"password_mismatch",
		)
		return
	}

	cfg := config.Get()
	mode := cfg.Auth.RegistrationMode

	switch mode {
	case "disabled":
		writeError(
			w,
			r,
			http.StatusForbidden,
			"Registration is disabled",
			"registration_disabled",
		)
	case "open":
		var (
			tok string
			err error
		)
		if body.Token != "" {
			_, tok, err = h.auth.RegisterWithInvite(
				r.Context(),
				body.Token,
				email,
				body.Password,
				body.DisplayName,
				sessionMeta(r),
			)
		} else {
			_, tok, err = h.auth.RegisterOpen(
				r.Context(),
				email,
				body.Password,
				body.DisplayName,
				cfg.Auth.OIDCDefaultRole,
				sessionMeta(r),
			)
		}
		if err != nil {
			slog.WarnContext(
				r.Context(),
				"register failed",
				"mode",
				"open",
				"error",
				err,
			)
			writeError(
				w,
				r,
				http.StatusBadRequest,
				userFacingRegisterError(err),
				"register_failed",
			)
			return
		}
		auth.SetSession(w, r, tok, h.sessionTTL())
		w.WriteHeader(http.StatusNoContent)
	case "invite":
		if body.Token == "" {
			writeError(
				w,
				r,
				http.StatusForbidden,
				"Invite required",
				"invite_required",
			)
			return
		}
		if email == "" {
			writeError(
				w,
				r,
				http.StatusBadRequest,
				"Email is required",
				"email_required",
			)
			return
		}
		_, tok, regErr := h.auth.RegisterWithInvite(
			r.Context(),
			body.Token,
			email,
			body.Password,
			body.DisplayName,
			sessionMeta(r),
		)
		if regErr != nil {
			slog.WarnContext(
				r.Context(),
				"register failed",
				"mode",
				"invite",
				"error",
				regErr,
			)
			writeError(
				w,
				r,
				http.StatusBadRequest,
				userFacingRegisterError(regErr),
				"register_failed",
			)
			return
		}
		auth.SetSession(w, r, tok, h.sessionTTL())
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(
			w,
			r,
			http.StatusForbidden,
			"Registration is disabled",
			"registration_disabled",
		)
	}
}

func (h *Handler) authLogout(w http.ResponseWriter, r *http.Request) {
	if claims := auth.ClaimsFromContext(
		r.Context(),
	); claims != nil &&
		claims.JTI != "" {
		if err := h.auth.RevokeSession(r.Context(), claims.JTI); err != nil {
			slog.WarnContext(r.Context(), "revoke session on logout failed",
				"session.id_hash", auth.JTILogValue(claims.JTI), "error", err)
		}
	}
	auth.ClearSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sessionTTL() time.Duration {
	cfg := config.Get()
	if cfg == nil {
		return defaultSessionTTL
	}
	d, err := time.ParseDuration(cfg.Auth.SessionTTL)
	if err != nil {
		return defaultSessionTTL
	}
	return d
}

func (h *Handler) allowAttempt(w http.ResponseWriter, r *http.Request) bool {
	if h.limiter == nil {
		return true
	}
	ip := httputil.ClientIPString(r)
	if h.limiter.Allow(ip) {
		return true
	}
	w.Header().Set("Retry-After",
		strconv.FormatInt(int64(h.limiter.RetryAfter(ip).Seconds()), 10))
	writeError(
		w,
		r,
		http.StatusTooManyRequests,
		"Too many attempts. Try again later.",
		"rate_limited",
	)
	return false
}

// userFacingRegisterError maps a service error to a message safe for display.
// Internal details are logged separately by the caller.
func userFacingRegisterError(err error) string {
	switch {
	case errors.Is(err, auth.ErrInviteInvalid):
		return "Invite invalid or expired"
	case ent.IsConstraintError(err):
		return "This email is already registered"
	default:
		return "Registration failed. Please try again."
	}
}

// lockedMessage formats a user-facing banner for a locked account. Rounds up
// to the nearest minute so "Try again in 0 minutes" never appears.
func lockedMessage(until time.Time) string {
	d := time.Until(until)
	if d <= 0 {
		return "Account temporarily locked. Try again shortly."
	}
	minutes := int((d + time.Minute - 1) / time.Minute)
	unit := "minutes"
	if minutes == 1 {
		unit = "minute"
	}
	return "Account temporarily locked. Try again in " + strconv.Itoa(
		minutes,
	) + " " + unit + "."
}

func sessionMeta(r *http.Request) auth.SessionMeta {
	ua := r.Header.Get("User-Agent")
	if ua == "" {
		ua = unknownUserAgent
	}
	return auth.SessionMeta{
		IP:        httputil.ClientIPString(r),
		UserAgent: ua,
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

const (
	oidcStateCookie    = "_oidc_state"
	oidcNonceCookie    = "_oidc_nonce"
	oidcVerifierCookie = "_oidc_pkce_verifier"
	oidcNextCookie     = "_oidc_next"
)

// sanitizeNext accepts an in-app return-to path. Rejects empty, non-rooted,
// and protocol-relative values so callers can't trick the server into
// redirecting to an external origin, plus /auth/* paths so a stale
// next=/auth/oidc/<name>/start can't re-launch the SSO flow after login.
func sanitizeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") ||
		strings.HasPrefix(n, "/auth/") {
		return defaultLandingURL
	}
	return n
}

// oidcRedirectURI builds the OIDC callback URL from the host the request
// arrived on (proxy X-Forwarded-* headers, else r.Host) rather than a single
// configured public URL, so the flow works across every domain streamline is
// exposed on. The callback lands back on the same host, so /start and
// /callback recompute the identical string OAuth requires. The IdP only honors
// registered redirect_uris, so a spoofed Host simply fails there.
func oidcRedirectURI(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + "/auth/oidc/" + name + "/callback"
}

func (h *Handler) oidcStart(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "name")
	prv, ok := h.oidc.Get(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	next := sanitizeNext(r.URL.Query().Get("next"))
	state := random.Must(32)
	nonce := random.Must(32)
	verifier := random.Must(64)

	auth.SetTransientCookie(w, r, oidcStateCookie, state)
	auth.SetTransientCookie(w, r, oidcNonceCookie, nonce)
	auth.SetTransientCookie(w, r, oidcVerifierCookie, verifier)
	auth.SetTransientCookie(w, r, oidcNextCookie, next)

	url := prv.OAuth2.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("redirect_uri", oidcRedirectURI(r, name)),
	)
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	if !h.allowAttempt(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	prv, ok := h.oidc.Get(name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	cookies := auth.ReadTransientCookies(r,
		oidcStateCookie, oidcNonceCookie, oidcVerifierCookie, oidcNextCookie)

	clearAll := func() {
		auth.ClearTransientCookies(w, r,
			oidcStateCookie, oidcNonceCookie, oidcVerifierCookie, oidcNextCookie)
	}

	if cookies[oidcStateCookie] == "" {
		clearAll()
		oidcFail(w, r, "oidc_state_missing")
		return
	}
	if cookies[oidcStateCookie] != r.URL.Query().Get("state") {
		clearAll()
		oidcFail(w, r, "oidc_state_mismatch")
		return
	}

	tok, err := prv.OAuth2.Exchange(r.Context(), r.URL.Query().Get("code"),
		oauth2.VerifierOption(cookies[oidcVerifierCookie]),
		oauth2.SetAuthURLParam("redirect_uri", oidcRedirectURI(r, name)))
	if err != nil {
		slog.WarnContext(
			r.Context(),
			"oidc exchange failed",
			"err",
			err,
			"provider",
			name,
		)
		clearAll()
		oidcFail(w, r, "oidc_provider_error")
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		clearAll()
		oidcFail(w, r, "oidc_provider_error")
		return
	}
	idTok, err := prv.Verifier.Verify(r.Context(), raw)
	if err != nil {
		slog.WarnContext(
			r.Context(),
			"oidc id_token verify failed",
			"err",
			err,
			"provider",
			name,
		)
		clearAll()
		oidcFail(w, r, "oidc_provider_error")
		return
	}
	if idTok.Nonce != cookies[oidcNonceCookie] {
		clearAll()
		oidcFail(w, r, "oidc_nonce_mismatch")
		return
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idTok.Claims(&claims); err != nil {
		clearAll()
		oidcFail(w, r, "oidc_provider_error")
		return
	}
	// Raw claims drive optional claim-based role mapping (auth.oidc[].role_claim).
	var rawClaims map[string]any
	if err := idTok.Claims(&rawClaims); err != nil {
		clearAll()
		oidcFail(w, r, "oidc_provider_error")
		return
	}

	_, sessTok, err := h.auth.LoginOIDC(
		r.Context(),
		name,
		claims.Sub,
		claims.Email,
		claims.Name,
		claims.EmailVerified,
		rawClaims,
		sessionMeta(r),
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrOIDCEmailUnverified):
			clearAll()
			oidcFail(w, r, "oidc_email_unverified")
		case errors.Is(err, auth.ErrOIDCRegDisabled):
			clearAll()
			oidcFail(w, r, "oidc_registration_disabled")
		case errors.Is(err, auth.ErrOIDCNoInvite):
			clearAll()
			oidcFail(w, r, "oidc_no_invite")
		default:
			slog.ErrorContext(
				r.Context(),
				"oidc login failed",
				"err",
				err,
				"provider",
				name,
			)
			clearAll()
			oidcFail(w, r, "oidc_provider_error")
		}
		return
	}

	clearAll()
	auth.SetSession(w, r, sessTok, h.sessionTTL())
	http.Redirect(w, r, sanitizeNext(cookies[oidcNextCookie]), http.StatusFound)
}

// oidcFail redirects back to the SPA login route with an error code. The SPA
// reads ?error= and renders the matching user-facing message.
func oidcFail(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+code, http.StatusFound)
}
