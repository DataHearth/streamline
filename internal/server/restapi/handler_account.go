package restapi

import (
	"context"
	"errors"

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
	dn := ""
	if req.Body != nil && req.Body.DisplayName != nil {
		dn = *req.Body.DisplayName
	}
	u, err := s.auth.UpdateProfile(ctx, claims.UserID, dn)
	if err != nil {
		return nil, err
	}
	return UpdateMe200JSONResponse(toAPIUser(u)), nil
}

// ChangePassword verifies the current password, rotates to the new one, and
// signs out every other active session for the caller.
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
	raw, rec, err := s.auth.CreateAPIKey(ctx, claims.UserID, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return CreateMyApiKey201JSONResponse{
		Id:        rec.ID,
		Name:      rec.Name,
		CreatedAt: rec.CreateTime,
		RawToken:  raw,
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
