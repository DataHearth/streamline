package restapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/restart"
)

// GetConfigAuth returns the runtime-safe auth configuration. Admin only.
func (s *Server) GetConfigAuth(
	ctx context.Context,
	_ GetConfigAuthRequestObject,
) (GetConfigAuthResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigAuth403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigAuth200JSONResponse{
		AuthConfigJSONResponse: authConfigView(config.Get().Auth),
	}, nil
}

// UpdateConfigAuth applies a partial update to the auth config. Admin only.
// Changes take effect immediately — no restart required.
func (s *Server) UpdateConfigAuth(
	ctx context.Context,
	req UpdateConfigAuthRequestObject,
) (UpdateConfigAuthResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigAuth403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	patch := config.AuthPatch{}
	if req.Body.RegistrationMode != nil {
		v := string(*req.Body.RegistrationMode)
		patch.RegistrationMode = &v
	}
	if req.Body.SessionTtl != nil {
		patch.SessionTTL = req.Body.SessionTtl
	}
	if req.Body.OidcDefaultRole != nil {
		v := string(*req.Body.OidcDefaultRole)
		patch.OIDCDefaultRole = &v
	}

	updated, err := config.UpdateAuth(ctx, patch)
	if configLocked(err) {
		return UpdateConfigAuth403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigAuth422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	slog.InfoContext(ctx, "auth config updated",
		"registration_mode", updated.RegistrationMode,
		"session_ttl", updated.SessionTTL,
		"oidc_default_role", updated.OIDCDefaultRole,
	)
	return UpdateConfigAuth200JSONResponse{
		AuthConfigJSONResponse: authConfigView(updated),
	}, nil
}

// ListOIDCProviders returns every configured provider plus the process-wide
// restart-required flag. Admin only.
func (s *Server) ListOIDCProviders(
	ctx context.Context,
	_ ListOIDCProvidersRequestObject,
) (ListOIDCProvidersResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListOIDCProviders403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	providers := config.Get().Auth.OIDC
	items := make([]OIDCProviderView, 0, len(providers))
	for _, p := range providers {
		items = append(items, oidcProviderView(p))
	}
	return ListOIDCProviders200JSONResponse{
		OIDCProviderListJSONResponse: OIDCProviderListJSONResponse{
			Providers:       items,
			RestartRequired: restart.Pending(),
		},
	}, nil
}

// GetOIDCProvider returns a single provider by name. Admin only.
func (s *Server) GetOIDCProvider(
	ctx context.Context,
	req GetOIDCProviderRequestObject,
) (GetOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	for _, p := range config.Get().Auth.OIDC {
		if p.Name == req.Name {
			return GetOIDCProvider200JSONResponse{
				OIDCProviderJSONResponse: OIDCProviderJSONResponse(
					oidcProviderView(p),
				),
			}, nil
		}
	}
	return GetOIDCProvider404JSONResponse{
		NotFoundJSONResponse: errNotFound("oidc provider not found"),
	}, nil
}

// CreateOIDCProvider validates the issuer via OIDC discovery and persists the
// provider. Success flips the process-wide restart-required flag because the
// runtime OIDC manager is initialised at startup.
func (s *Server) CreateOIDCProvider(
	ctx context.Context,
	req CreateOIDCProviderRequestObject,
) (CreateOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	p := config.OIDCConfig{
		Name:         req.Body.Name,
		Issuer:       req.Body.Issuer,
		ClientID:     req.Body.ClientId,
		ClientSecret: req.Body.ClientSecret,
	}
	switch err := config.AddOIDCProvider(ctx, p); {
	case errors.Is(err, config.ErrOIDCProviderExists):
		return CreateOIDCProvider409JSONResponse{
			ConflictJSONResponse: errConflict("oidc provider name already exists"),
		}, nil
	case errors.Is(err, config.ErrOIDCDiscoveryFailed):
		return CreateOIDCProvider422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case configLocked(err):
		return CreateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		p.Name,
		"action",
		"create",
	)
	return CreateOIDCProvider201JSONResponse{
		OIDCProviderCreatedJSONResponse: OIDCProviderCreatedJSONResponse(
			oidcProviderView(p),
		),
	}, nil
}

// UpdateOIDCProvider merges patch fields into the named provider. Blank
// client_secret preserves the existing secret. Flips the restart flag.
func (s *Server) UpdateOIDCProvider(
	ctx context.Context,
	req UpdateOIDCProviderRequestObject,
) (UpdateOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	patch := config.OIDCProviderPatch{
		Issuer:       req.Body.Issuer,
		ClientID:     req.Body.ClientId,
		ClientSecret: req.Body.ClientSecret,
	}
	switch err := config.UpdateOIDCProvider(ctx, req.Name, patch); {
	case errors.Is(err, config.ErrOIDCProviderNotFound):
		return UpdateOIDCProvider404JSONResponse{
			NotFoundJSONResponse: errNotFound("oidc provider not found"),
		}, nil
	case errors.Is(err, config.ErrOIDCDiscoveryFailed):
		return UpdateOIDCProvider422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		req.Name,
		"action",
		"update",
	)
	for _, p := range config.Get().Auth.OIDC {
		if p.Name == req.Name {
			return UpdateOIDCProvider200JSONResponse{
				OIDCProviderJSONResponse: OIDCProviderJSONResponse(
					oidcProviderView(p),
				),
			}, nil
		}
	}
	// Should not happen — Update succeeded so the provider exists.
	return UpdateOIDCProvider404JSONResponse{
		NotFoundJSONResponse: errNotFound("oidc provider not found"),
	}, nil
}

// DeleteOIDCProvider removes the named provider. Flips the restart flag.
func (s *Server) DeleteOIDCProvider(
	ctx context.Context,
	req DeleteOIDCProviderRequestObject,
) (DeleteOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteOIDCProvider(ctx, req.Name); {
	case errors.Is(err, config.ErrOIDCProviderNotFound):
		return DeleteOIDCProvider404JSONResponse{
			NotFoundJSONResponse: errNotFound("oidc provider not found"),
		}, nil
	case configLocked(err):
		return DeleteOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		req.Name,
		"action",
		"delete",
	)
	return DeleteOIDCProvider204Response{}, nil
}

// authConfigView maps config.AuthConfig into the generated AuthConfigView.
func authConfigView(a config.AuthConfig) AuthConfigJSONResponse {
	return AuthConfigJSONResponse{
		RegistrationMode: AuthConfigViewRegistrationMode(a.RegistrationMode),
		SessionTtl:       a.SessionTTL,
		OidcDefaultRole:  AuthConfigViewOidcDefaultRole(a.OIDCDefaultRole),
	}
}

// oidcProviderView maps config.OIDCConfig into the generated OIDCProviderView.
// The raw secret never leaves the process — only a "configured" flag is emitted.
func oidcProviderView(p config.OIDCConfig) OIDCProviderView {
	return OIDCProviderView{
		Name:            p.Name,
		Issuer:          p.Issuer,
		ClientId:        p.ClientID,
		ClientSecretSet: p.ClientSecret != "" || p.ClientSecretFile != "",
	}
}
