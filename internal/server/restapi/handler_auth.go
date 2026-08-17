package restapi

import (
	"context"
	"errors"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) AuthMe(
	ctx context.Context,
	_ AuthMeRequestObject,
) (AuthMeResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return AuthMe401JSONResponse{
			UnauthorizedJSONResponse: UnauthorizedJSONResponse{
				Message: "unauthorized",
			},
		}, nil
	}
	u, err := s.auth.GetUserByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return AuthMe401JSONResponse{
				UnauthorizedJSONResponse: UnauthorizedJSONResponse{
					Message: "unauthorized",
				},
			}, nil
		}
		return nil, err
	}
	return AuthMe200JSONResponse(toAPIUser(u)), nil
}

func (s *Server) ListInvites(
	ctx context.Context,
	_ ListInvitesRequestObject,
) (ListInvitesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListInvites403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	list, err := s.auth.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(list))
	for _, inv := range list {
		out = append(out, toAPIInvite(inv))
	}
	return ListInvites200JSONResponse(out), nil
}

func (s *Server) CreateInvite(
	ctx context.Context,
	req CreateInviteRequestObject,
) (CreateInviteResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateInvite403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	c := auth.ClaimsFromContext(ctx)

	ttl := 7 * 24 * time.Hour
	if req.Body.Ttl != nil && *req.Body.Ttl != "" {
		if d, err := time.ParseDuration(*req.Body.Ttl); err == nil {
			ttl = d
		}
	}
	raw, inv, err := s.auth.CreateInvite(
		ctx,
		c.UserID,
		string(req.Body.Email),
		string(req.Body.Role),
		ttl,
	)
	if errors.Is(err, auth.ErrRegistrationDisabled) {
		return CreateInvite403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var invEmail *openapi_types.Email
	if inv.Email != "" {
		e := openapi_types.Email(inv.Email)
		invEmail = &e
	}
	return CreateInvite201JSONResponse{
		Id:        inv.ID,
		Email:     invEmail,
		Role:      InviteCreatedRole(inv.Role),
		ExpiresAt: inv.ExpiresAt,
		UsedAt:    inv.UsedAt,
		CreatedAt: inv.CreateTime,
		RawToken:  raw,
		Url:       s.publicInviteURL(raw),
	}, nil
}

// RotateJWTSecret generates a fresh signing secret, atomically swaps it in
// memory, persists it to config, truncates every session, and returns a
// freshly-issued bearer token for the calling admin so they stay signed in.
func (s *Server) RotateJWTSecret(
	ctx context.Context,
	req RotateJWTSecretRequestObject,
) (RotateJWTSecretResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RotateJWTSecret403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil {
		return RotateJWTSecret403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	// Read-only can't persist the secret, so the operator has to copy it into
	// the config they manage before the swap — hand it out first, apply only
	// once they confirm. See PrepareJWTRotation.
	if config.Get().ReadOnly {
		return s.rotateJWTReadOnly(ctx, claims.UserID, req)
	}

	tok, err := s.auth.RotateJWTSecret(ctx, claims.UserID)
	if configLocked(err) {
		return RotateJWTSecret403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return RotateJWTSecret200JSONResponse{
		JWTRotatedJSONResponse: JWTRotatedJSONResponse{Token: &tok},
	}, nil
}

func (s *Server) rotateJWTReadOnly(
	ctx context.Context,
	callerID uint32,
	req RotateJWTSecretRequestObject,
) (RotateJWTSecretResponseObject, error) {
	confirmed := req.Body != nil && req.Body.Confirmed != nil &&
		*req.Body.Confirmed
	if !confirmed {
		secret, err := s.auth.PrepareJWTRotation(ctx, callerID)
		if err != nil {
			return nil, err
		}
		pending := true
		return RotateJWTSecret200JSONResponse{
			JWTRotatedJSONResponse: JWTRotatedJSONResponse{
				Pending: &pending,
				Secret:  &secret,
			},
		}, nil
	}
	tok, err := s.auth.ConfirmJWTRotation(ctx, callerID)
	if errors.Is(err, auth.ErrNoPendingRotation) {
		return RotateJWTSecret403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return RotateJWTSecret200JSONResponse{
		JWTRotatedJSONResponse: JWTRotatedJSONResponse{Token: &tok},
	}, nil
}

func (s *Server) RevokeInvite(
	ctx context.Context,
	req RevokeInviteRequestObject,
) (RevokeInviteResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RevokeInvite403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := s.auth.RevokeInvite(ctx, req.Id); err != nil {
		return RevokeInvite404JSONResponse{
			NotFoundJSONResponse: NotFoundJSONResponse{Message: "not found"},
		}, nil
	}
	return RevokeInvite204Response{}, nil
}

func requireAdmin(ctx context.Context) error {
	if !auth.IsAdmin(ctx) {
		return errNotAdmin
	}
	return nil
}

// requireNotRequestOnly rejects request_only callers from direct library
// mutations (adding a movie/series). They may only submit requests.
func requireNotRequestOnly(ctx context.Context) error {
	c := auth.ClaimsFromContext(ctx)
	if c == nil || c.Role == "request_only" {
		return errRequestOnly
	}
	return nil
}

// publicInviteURL builds the absolute registration URL included in the
// CreateInvite response. Uses PublicBaseURL (STREAMLINE_PUBLIC_URL or
// server host:port fallback).
func (s *Server) publicInviteURL(rawToken string) string {
	return s.publicURL + "/register?token=" + rawToken
}
