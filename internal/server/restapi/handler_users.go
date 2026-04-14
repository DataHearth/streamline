package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
)

// ListUsers returns a paginated, filtered slice of users. Admin-only.
func (s *Server) ListUsers(
	ctx context.Context,
	req ListUsersRequestObject,
) (ListUsersResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListUsers403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	f := auth.UserFilter{}
	if req.Params.Q != nil {
		f.Q = *req.Params.Q
	}
	if req.Params.Role != nil {
		f.Role = string(*req.Params.Role)
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Offset != nil {
		f.Offset = *req.Params.Offset
	}
	if req.Params.Sort != nil {
		switch *req.Params.Sort {
		case ListUsersParamsSortName:
			f.Sort = db.UserSortName
		case ListUsersParamsSortRole:
			f.Sort = db.UserSortRole
		case ListUsersParamsSortAuth:
			f.Sort = db.UserSortAuth
		default:
			f.Sort = db.UserSortCreated
		}
	}
	if req.Params.Order != nil && *req.Params.Order == ListUsersParamsOrderAsc {
		f.Order = db.UserOrderAsc
	}
	items, total, err := s.auth.ListUsers(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(items))
	for _, u := range items {
		out = append(out, toAPIUser(u))
	}
	return ListUsers200JSONResponse{
		UsersListJSONResponse: UsersListJSONResponse{
			Items: out,
			Total: uint32(total),
		},
	}, nil
}

// CreateUser directly provisions a user (bypassing invites). Admin-only.
func (s *Server) CreateUser(
	ctx context.Context,
	req CreateUserRequestObject,
) (CreateUserResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateUser403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	dn := ""
	if req.Body.DisplayName != nil {
		dn = *req.Body.DisplayName
	}
	u, err := s.auth.CreateUserDirect(
		ctx,
		string(req.Body.Email),
		req.Body.Password,
		string(req.Body.Role),
		dn,
	)
	switch {
	case errors.Is(err, auth.ErrUserEmailExists):
		return CreateUser409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"email_exists",
				"email already registered",
			),
		}, nil
	case errors.Is(err, auth.ErrPasswordWeak):
		return CreateUser422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"password does not meet minimum policy",
			),
		}, nil
	case err != nil:
		return nil, err
	}
	return CreateUser201JSONResponse{
		UserCreatedJSONResponse: UserCreatedJSONResponse(toAPIUser(u)),
	}, nil
}

// GetUser returns the user's detail block (user + api keys + sessions).
// Admin-only. Sessions are rendered with is_current=false in the admin view.
func (s *Server) GetUser(
	ctx context.Context,
	req GetUserRequestObject,
) (GetUserResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetUser403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	u, keys, sessions, err := s.auth.GetUserDetail(ctx, req.Uid)
	if errors.Is(err, auth.ErrUserNotFound) {
		return GetUser404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	apiKeys := make([]ApiKey, 0, len(keys))
	for _, k := range keys {
		apiKeys = append(apiKeys, toAPIApiKey(k))
	}
	apiSessions := make([]Session, 0, len(sessions))
	for _, sess := range sessions {
		// No "current" concept when an admin views another user; pass empty
		// jti so is_current is always false.
		apiSessions = append(apiSessions, toAPISession(sess, ""))
	}
	return GetUser200JSONResponse{
		UserDetailJSONResponse: UserDetailJSONResponse{
			User:     toAPIUser(u),
			ApiKeys:  apiKeys,
			Sessions: apiSessions,
		},
	}, nil
}

// UpdateUser applies a partial patch to the user (role, display_name,
// auth_method). Admin-only. Demoting the last admin is rejected with 409
// last_admin.
func (s *Server) UpdateUser(
	ctx context.Context,
	req UpdateUserRequestObject,
) (UpdateUserResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateUser403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	patch := auth.UserPatch{}
	if req.Body.Email != nil {
		e := string(*req.Body.Email)
		patch.Email = &e
	}
	if req.Body.Role != nil {
		r := string(*req.Body.Role)
		patch.Role = &r
	}
	if req.Body.DisplayName != nil {
		patch.DisplayName = req.Body.DisplayName
	}
	if req.Body.AuthMethod != nil {
		a := string(*req.Body.AuthMethod)
		patch.AuthMethod = &a
	}
	switch err := s.auth.UpdateUser(ctx, req.Uid, patch); {
	case errors.Is(err, auth.ErrUserNotFound):
		return UpdateUser404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	case errors.Is(err, auth.ErrUserEmailExists):
		return UpdateUser409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"email_exists",
				"email already registered",
			),
		}, nil
	case errors.Is(err, auth.ErrLastAdmin):
		return UpdateUser409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"last_admin",
				"cannot remove the last admin",
			),
		}, nil
	case err != nil:
		return nil, err
	}
	u, err := s.auth.GetUserByID(ctx, req.Uid)
	if err != nil {
		return nil, err
	}
	return UpdateUser200JSONResponse{
		UserUpdatedJSONResponse: UserUpdatedJSONResponse(toAPIUser(u)),
	}, nil
}

// DeleteUser permanently removes the user plus every owned resource (api
// keys, oidc identities, sessions, requests) via schema-level cascade.
// Admin-only; guards against self-delete, last-admin.
func (s *Server) DeleteUser(
	ctx context.Context,
	req DeleteUserRequestObject,
) (DeleteUserResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteUser403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	claims := auth.ClaimsFromContext(ctx)
	err := s.auth.DeleteUser(ctx, req.Uid, claims.UserID)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return DeleteUser404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	case errors.Is(err, auth.ErrSelfDeleteForbidden):
		return DeleteUser409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"self_delete_forbidden",
				"cannot delete yourself",
			),
		}, nil
	case errors.Is(err, auth.ErrLastAdmin):
		return DeleteUser409JSONResponse{
			ConflictJSONResponse: conflictResp(
				"last_admin",
				"cannot delete the last admin",
			),
		}, nil
	case err != nil:
		return nil, err
	}
	return DeleteUser204Response{}, nil
}

// ResetUserPassword rotates the target's password without verifying the old
// one and revokes every one of their sessions. Admin-only.
func (s *Server) ResetUserPassword(
	ctx context.Context,
	req ResetUserPasswordRequestObject,
) (ResetUserPasswordResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ResetUserPassword403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := s.auth.AdminResetPassword(ctx, req.Uid, req.Body.NewPassword)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return ResetUserPassword404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	case errors.Is(err, auth.ErrPasswordWeak):
		return ResetUserPassword422JSONResponse{
			UnprocessableEntityJSONResponse: unprocessableResp(
				"password does not meet minimum policy",
			),
		}, nil
	case err != nil:
		return nil, err
	}
	return ResetUserPassword204Response{}, nil
}

// RevokeUserApiKey revokes a specific API key belonging to the target user.
// Admin-only. Returns 404 when the key does not exist or does not belong to
// the user.
func (s *Server) RevokeUserApiKey(
	ctx context.Context,
	req RevokeUserApiKeyRequestObject,
) (RevokeUserApiKeyResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RevokeUserApiKey403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.auth.AdminRevokeAPIKey(ctx, req.Uid, req.Kid); err != nil {
		if errors.Is(err, auth.ErrAPIKeyNotFound) {
			return RevokeUserApiKey404JSONResponse{
				NotFoundJSONResponse: notFoundResp("api key not found"),
			}, nil
		}
		return nil, err
	}
	return RevokeUserApiKey204Response{}, nil
}

// RevokeUserSession revokes a specific session belonging to the target user,
// forcing logout on that device. Admin-only. Returns 404 when the session
// does not exist or does not belong to the user.
func (s *Server) RevokeUserSession(
	ctx context.Context,
	req RevokeUserSessionRequestObject,
) (RevokeUserSessionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RevokeUserSession403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.auth.AdminRevokeSession(ctx, req.Uid, req.Sid); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return RevokeUserSession404JSONResponse{
				NotFoundJSONResponse: notFoundResp("session not found"),
			}, nil
		}
		return nil, err
	}
	return RevokeUserSession204Response{}, nil
}

// UnlockUser clears every lockout field on the target user. Admin-only.
// Idempotent — returns 204 even when the user wasn't locked.
func (s *Server) UnlockUser(
	ctx context.Context,
	req UnlockUserRequestObject,
) (UnlockUserResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UnlockUser403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.auth.AdminUnlock(ctx, req.Uid); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return UnlockUser404JSONResponse{
				NotFoundJSONResponse: notFoundResp("user not found"),
			}, nil
		}
		return nil, err
	}
	return UnlockUser204Response{}, nil
}
