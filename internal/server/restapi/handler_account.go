package restapi

import (
	"context"
	"errors"
	"time"

	"github.com/datahearth/streamline/ent/apikey"
	"github.com/datahearth/streamline/internal/auth"
)

// UpdateMe patches the current user's self-service profile fields.
func (s *Server) UpdateMe(
	ctx context.Context,
	req UpdateMeRequestObject,
) (UpdateMeResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return UpdateMe401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	var patch auth.ProfilePatch
	if req.Body != nil {
		patch.DisplayName = req.Body.DisplayName
		patch.DeviceEmail = req.Body.DeviceEmail
	}
	u, err := s.auth.UpdateProfile(ctx, claims.UserID, patch)
	switch {
	case err == nil:
		return UpdateMe200JSONResponse(toAPIUserWithSend(u)), nil
	case errors.Is(err, auth.ErrInvalidDeviceEmail):
		return UpdateMe422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(err.Error()),
		}, nil
	default:
		return nil, err
	}
}

// ChangePassword verifies the current password, rotates to the new one, signs
// out every other active session for the caller, and revokes all of their API
// keys, including one used to authenticate this request.
func (s *Server) ChangePassword(
	ctx context.Context,
	req ChangePasswordRequestObject,
) (ChangePasswordResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return ChangePassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	err := s.auth.ChangePassword(
		ctx,
		claims.UserID,
		req.Body.CurrentPassword,
		req.Body.NewPassword,
		claims.JTI,
	)
	switch {
	case err == nil:
		return ChangePassword204Response{}, nil
	case errors.Is(err, auth.ErrPasswordInvalid):
		return ChangePassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("current password invalid"),
		}, nil
	case errors.Is(err, auth.ErrPasswordWeak):
		return ChangePassword422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"new password does not meet minimum policy",
			),
		}, nil
	default:
		return nil, err
	}
}

// ListMyApiKeys returns every API key owned by the caller. Raw tokens are
// never returned — only the metadata surfaced in the ApiKey schema.
func (s *Server) ListMyApiKeys(
	ctx context.Context,
	_ ListMyApiKeysRequestObject,
) (ListMyApiKeysResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return ListMyApiKeys401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	keys, err := s.auth.ListAPIKeys(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]ApiKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPIApiKey(k))
	}
	return ListMyApiKeys200JSONResponse(out), nil
}

// CreateMyApiKey generates a new API key for the caller. The raw token is
// returned exactly once — clients must surface it immediately.
func (s *Server) CreateMyApiKey(
	ctx context.Context,
	req CreateMyApiKeyRequestObject,
) (CreateMyApiKeyResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return CreateMyApiKey401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	// API-key auth carries no JTI. Only a session may mint keys, so a leaked
	// key cannot create replacements that would outlive its own revocation.
	if claims.JTI == "" {
		return CreateMyApiKey401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required to create API keys",
			),
		}, nil
	}
	spec := auth.APIKeySpec{Name: req.Body.Name, Scope: apikey.ScopeFull}
	if sc := req.Body.Scope; sc != nil {
		if !sc.Valid() {
			return CreateMyApiKey422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(
					"scope must be read or full",
				),
			}, nil
		}
		spec.Scope = apikey.Scope(*sc)
	}
	if days := req.Body.ExpiresInDays; days != nil {
		// Nothing validates a body against the spec, so the presets are held
		// here: an arbitrary lifetime would let a key outlive any policy the
		// account page states.
		if !days.Valid() {
			return CreateMyApiKey422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(
					"expires_in_days must be 30, 90 or 365",
				),
			}, nil
		}
		spec.TTL = time.Duration(*days) * 24 * time.Hour
	}
	raw, rec, err := s.auth.CreateAPIKey(ctx, claims.UserID, spec)
	switch {
	case errors.Is(err, auth.ErrAPIKeyQuotaExceeded):
		code := codeAPIKeyQuota
		return CreateMyApiKey422JSONResponse{
			Message: err.Error(),
			Code:    &code,
		}, nil
	case errors.Is(err, auth.ErrAPIKeyInvalid):
		return CreateMyApiKey422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	out := toAPIApiKey(rec)
	return CreateMyApiKey201JSONResponse{
		Id:         out.Id,
		Name:       out.Name,
		Scope:      out.Scope,
		ExpiresAt:  out.ExpiresAt,
		LastUsedAt: out.LastUsedAt,
		CreatedAt:  out.CreatedAt,
		RawToken:   raw,
	}, nil
}

// DeleteMyApiKey revokes one of the caller's API keys. Foreign/missing IDs
// surface 404 — callers must not leak ownership information.
func (s *Server) DeleteMyApiKey(
	ctx context.Context,
	req DeleteMyApiKeyRequestObject,
) (DeleteMyApiKeyResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return DeleteMyApiKey401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if err := s.auth.RevokeAPIKeyByID(ctx, claims.UserID, req.Id); err != nil {
		if errors.Is(err, auth.ErrAPIKeyNotFound) {
			return DeleteMyApiKey404JSONResponse{
				NotFoundJSONResponse: notFoundResp("not found"),
			}, nil
		}
		return nil, err
	}
	return DeleteMyApiKey204Response{}, nil
}

// ListMySessions returns every session row for the caller so the account UI
// can render "active sessions" with the current one flagged.
func (s *Server) ListMySessions(
	ctx context.Context,
	_ ListMySessionsRequestObject,
) (ListMySessionsResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return ListMySessions401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	sessions, err := s.auth.ListUserSessions(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, toAPISession(sess, claims.JTI))
	}
	return ListMySessions200JSONResponse(out), nil
}

// DeleteMySession revokes one of the caller's sessions. Revoking the current
// session is allowed — the next request will fail middleware and redirect to
// /login. Foreign/missing IDs surface 404.
func (s *Server) DeleteMySession(
	ctx context.Context,
	req DeleteMySessionRequestObject,
) (DeleteMySessionResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return DeleteMySession401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if err := s.auth.RevokeSessionByID(ctx, claims.UserID, req.Id); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return DeleteMySession404JSONResponse{
				NotFoundJSONResponse: notFoundResp("not found"),
			}, nil
		}
		return nil, err
	}
	return DeleteMySession204Response{}, nil
}

// GetSubsonicPassword reports the caller's Subsonic access. Session-only,
// like the rest of the credential endpoints. It never carries the password.
func (s *Server) GetSubsonicPassword(
	ctx context.Context,
	_ GetSubsonicPasswordRequestObject,
) (GetSubsonicPasswordResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return GetSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return GetSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	u, err := s.auth.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	return GetSubsonicPassword200JSONResponse(toAppAccess(
		u.SubsonicPassword != "",
		u.Email,
		u.SubsonicCreatedAt,
		u.SubsonicLastUsedAt,
		u.SubsonicLastClient,
	)), nil
}

// RotateSubsonicPassword generates a new Subsonic password for the caller.
func (s *Server) RotateSubsonicPassword(
	ctx context.Context,
	_ RotateSubsonicPasswordRequestObject,
) (RotateSubsonicPasswordResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return RotateSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return RotateSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	pw, err := s.auth.RotateSubsonicPassword(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	u, err := s.auth.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	return RotateSubsonicPassword200JSONResponse(toAppAccessCreated(
		toAppAccess(
			true,
			u.Email,
			u.SubsonicCreatedAt,
			u.SubsonicLastUsedAt,
			u.SubsonicLastClient,
		),
		pw,
	)), nil
}

// DisableSubsonicPassword clears the caller's Subsonic password.
func (s *Server) DisableSubsonicPassword(
	ctx context.Context,
	_ DisableSubsonicPasswordRequestObject,
) (DisableSubsonicPasswordResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return DisableSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return DisableSubsonicPassword401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	if err := s.auth.DisableSubsonicPassword(ctx, claims.UserID); err != nil {
		return nil, err
	}
	return DisableSubsonicPassword204Response{}, nil
}

// GetOpdsToken reports the caller's OPDS access. Session-only, and the token
// is stored hashed, so there is nothing to return but its state.
func (s *Server) GetOpdsToken(
	ctx context.Context,
	_ GetOpdsTokenRequestObject,
) (GetOpdsTokenResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return GetOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return GetOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	u, err := s.auth.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	return GetOpdsToken200JSONResponse(toAppAccess(
		u.OpdsToken != "",
		u.Email,
		u.OpdsCreatedAt,
		u.OpdsLastUsedAt,
		u.OpdsLastClient,
	)), nil
}

// RotateOpdsToken generates a new OPDS token for the caller.
func (s *Server) RotateOpdsToken(
	ctx context.Context,
	_ RotateOpdsTokenRequestObject,
) (RotateOpdsTokenResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return RotateOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return RotateOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	token, err := s.auth.RotateOPDSToken(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	u, err := s.auth.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	return RotateOpdsToken200JSONResponse(toAppAccessCreated(
		toAppAccess(
			true,
			u.Email,
			u.OpdsCreatedAt,
			u.OpdsLastUsedAt,
			u.OpdsLastClient,
		),
		token,
	)), nil
}

// DisableOpdsToken clears the caller's OPDS token.
func (s *Server) DisableOpdsToken(
	ctx context.Context,
	_ DisableOpdsTokenRequestObject,
) (DisableOpdsTokenResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return DisableOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp("unauthorized"),
		}, nil
	}
	if claims.JTI == "" {
		return DisableOpdsToken401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(
				"session authentication required",
			),
		}, nil
	}
	if err := s.auth.DisableOPDSToken(ctx, claims.UserID); err != nil {
		return nil, err
	}
	return DisableOpdsToken204Response{}, nil
}

func toAppAccess(
	enabled bool,
	email string,
	createdAt, lastUsedAt *time.Time,
	lastClient string,
) AppAccess {
	out := AppAccess{
		Enabled:    enabled,
		Username:   email,
		CreatedAt:  createdAt,
		LastUsedAt: lastUsedAt,
	}
	if lastClient != "" {
		out.LastClient = &lastClient
	}
	return out
}

func toAppAccessCreated(a AppAccess, secret string) AppAccessCreated {
	return AppAccessCreated{
		Enabled:    a.Enabled,
		Username:   a.Username,
		CreatedAt:  a.CreatedAt,
		LastUsedAt: a.LastUsedAt,
		LastClient: a.LastClient,
		Secret:     secret,
	}
}
