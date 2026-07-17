package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/restart"
)

func (s *Server) ListDownloadClients(
	ctx context.Context,
	_ ListDownloadClientsRequestObject,
) (ListDownloadClientsResponseObject, error) {
	c := config.Get()
	items := make([]DownloadClient, 0, len(c.DownloadClients))
	for _, e := range c.DownloadClients {
		d := downloadClientToAPI(e)
		if e.ClientType == "builtin" {
			s.applyBuiltinRuntime(&d)
		}
		items = append(items, d)
	}
	return ListDownloadClients200JSONResponse(items), nil
}

// applyBuiltinRuntime fills the read-only runtime fields (running / bound port /
// bound interface) on the builtin download-client view from the live engine.
// The engine is absent (nil) when the builtin entry is disabled or failed to
// start, which reads as running=false.
func (s *Server) applyBuiltinRuntime(d *DownloadClient) {
	running := s.torrents != nil
	d.Running = &running
	if s.torrents == nil {
		return
	}
	rt := s.torrents.Runtime()
	port := rt.PortBound
	d.PortBound = &port
	if rt.InterfaceBound != "" {
		iface := rt.InterfaceBound
		d.InterfaceBound = &iface
	}
}

func (s *Server) CreateDownloadClient(
	ctx context.Context,
	request CreateDownloadClientRequestObject,
) (CreateDownloadClientResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateDownloadClient403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := config.DownloadClientEntry{
		Name:       request.Body.Name,
		ClientType: string(request.Body.ClientType),
		AuthMethod: "password",
	}
	if request.Body.Host != nil {
		e.Host = *request.Body.Host
	}
	if request.Body.Port != nil {
		e.Port = *request.Body.Port
	}
	if request.Body.AuthMethod != nil {
		e.AuthMethod = string(*request.Body.AuthMethod)
	}
	if request.Body.Username != nil {
		e.Username = *request.Body.Username
	}
	if request.Body.Password != nil {
		e.Password = *request.Body.Password
	}
	if request.Body.ApiKey != nil {
		e.APIKey = *request.Body.ApiKey
	}
	if request.Body.UseSsl != nil {
		e.UseSSL = *request.Body.UseSsl
	}
	if request.Body.Priority != nil {
		e.Priority = *request.Body.Priority
	}
	if request.Body.Enabled != nil {
		e.Enabled = *request.Body.Enabled
	}
	if request.Body.DownloadDir != nil {
		e.DownloadDir = *request.Body.DownloadDir
	}
	if request.Body.ListenPort != nil {
		e.ListenPort = *request.Body.ListenPort
	}
	if request.Body.MaxUploadKbps != nil {
		e.MaxUploadKbps = *request.Body.MaxUploadKbps
	}
	if request.Body.MaxDownloadKbps != nil {
		e.MaxDownloadKbps = *request.Body.MaxDownloadKbps
	}
	if request.Body.SeedRatio != nil {
		e.SeedRatio = *request.Body.SeedRatio
	}
	if request.Body.SeedTime != nil {
		e.SeedTime = *request.Body.SeedTime
	}
	if request.Body.DisableDht != nil {
		e.DisableDHT = *request.Body.DisableDht
	}
	if request.Body.BindInterface != nil {
		e.BindInterface = *request.Body.BindInterface
	}

	switch err := config.AddDownloadClient(ctx, e); {
	case errors.Is(err, config.ErrDownloadClientExists):
		return CreateDownloadClient409JSONResponse{
			ConflictJSONResponse: errConflict("download client name already exists"),
		}, nil
	case configLocked(err):
		return CreateDownloadClient403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateDownloadClient422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if e.ClientType == "builtin" {
		restart.Mark()
	}
	return CreateDownloadClient201JSONResponse(downloadClientToAPI(e)), nil
}

func (s *Server) UpdateDownloadClient(
	ctx context.Context,
	request UpdateDownloadClientRequestObject,
) (UpdateDownloadClientResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateDownloadClient403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	ct := string(request.Body.ClientType)
	patch := config.DownloadClientPatch{
		ClientType:      &ct,
		Host:            request.Body.Host,
		Port:            request.Body.Port,
		Username:        request.Body.Username,
		Password:        request.Body.Password,
		APIKey:          request.Body.ApiKey,
		UseSSL:          request.Body.UseSsl,
		Priority:        request.Body.Priority,
		Enabled:         request.Body.Enabled,
		DownloadDir:     request.Body.DownloadDir,
		ListenPort:      request.Body.ListenPort,
		MaxUploadKbps:   request.Body.MaxUploadKbps,
		MaxDownloadKbps: request.Body.MaxDownloadKbps,
		SeedRatio:       request.Body.SeedRatio,
		SeedTime:        request.Body.SeedTime,
		DisableDHT:      request.Body.DisableDht,
		BindInterface:   request.Body.BindInterface,
	}
	if request.Body.AuthMethod != nil {
		am := string(*request.Body.AuthMethod)
		patch.AuthMethod = &am
	}

	prev, _ := config.FindDownloadClient(request.Name)

	switch err := config.UpdateDownloadClient(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrDownloadClientNotFound):
		return UpdateDownloadClient404JSONResponse{
			NotFoundJSONResponse: errNotFound("download client not found"),
		}, nil
	case configLocked(err):
		return UpdateDownloadClient403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateDownloadClient422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if prev.ClientType == "builtin" || string(request.Body.ClientType) == "builtin" {
		restart.Mark()
	}
	e, _ := config.FindDownloadClient(request.Name)
	return UpdateDownloadClient200JSONResponse(downloadClientToAPI(e)), nil
}

func (s *Server) DeleteDownloadClient(
	ctx context.Context,
	request DeleteDownloadClientRequestObject,
) (DeleteDownloadClientResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteDownloadClient403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	prev, _ := config.FindDownloadClient(request.Name)

	switch err := config.DeleteDownloadClient(ctx, request.Name); {
	case errors.Is(err, config.ErrDownloadClientNotFound):
		return DeleteDownloadClient404JSONResponse{
			NotFoundJSONResponse: errNotFound("download client not found"),
		}, nil
	case configLocked(err):
		return DeleteDownloadClient403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteDownloadClient500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	if prev.ClientType == "builtin" {
		restart.Mark()
	}
	return DeleteDownloadClient204Response{}, nil
}

func (s *Server) TestDownloadClient(
	ctx context.Context,
	request TestDownloadClientRequestObject,
) (TestDownloadClientResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestDownloadClient403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := s.downloads.TestByName(ctx, request.Name)
	switch {
	case err == nil:
		return TestDownloadClient200Response{}, nil
	case errors.Is(err, config.ErrDownloadClientNotFound):
		return TestDownloadClient404JSONResponse{
			NotFoundJSONResponse: errNotFound("download client not found"),
		}, nil
	case errors.Is(err, download.ErrUnsupportedClient),
		errors.Is(err, download.ErrUnreachable),
		errors.Is(err, download.ErrUnauthorized),
		errors.Is(err, download.ErrUnexpectedStatus),
		errors.Is(err, download.ErrBadResponse):
		return TestDownloadClient422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestDownloadClient500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
}

func (s *Server) TestDraftDownloadClient(
	ctx context.Context,
	request TestDraftDownloadClientRequestObject,
) (TestDraftDownloadClientResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestDraftDownloadClient403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	b := request.Body
	p := download.TestParams{
		ClientType: string(b.ClientType),
		AuthMethod: "password",
	}
	if b.Host != nil {
		p.Host = *b.Host
	}
	if b.Port != nil {
		p.Port = *b.Port
	}
	if b.AuthMethod != nil {
		p.AuthMethod = string(*b.AuthMethod)
	}
	if b.Username != nil {
		p.Username = *b.Username
	}
	if b.Password != nil {
		p.Password = *b.Password
	}
	if b.ApiKey != nil {
		p.APIKey = *b.ApiKey
	}
	if b.UseSsl != nil {
		p.UseSSL = *b.UseSsl
	}

	switch err := s.downloads.Test(ctx, p); {
	case err == nil:
		return TestDraftDownloadClient200Response{}, nil
	case errors.Is(err, download.ErrUnsupportedClient),
		errors.Is(err, download.ErrUnreachable),
		errors.Is(err, download.ErrUnauthorized),
		errors.Is(err, download.ErrUnexpectedStatus),
		errors.Is(err, download.ErrBadResponse):
		return TestDraftDownloadClient422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestDraftDownloadClient500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
}
