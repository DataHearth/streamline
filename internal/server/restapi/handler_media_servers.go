package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/mediaserver"
)

func (s *Server) ListMediaServers(
	ctx context.Context,
	_ ListMediaServersRequestObject,
) (ListMediaServersResponseObject, error) {
	servers := config.Get().MediaServer.Servers
	items := make([]MediaServer, 0, len(servers))
	for _, ms := range servers {
		items = append(items, mediaServerToAPI(ms))
	}
	return ListMediaServers200JSONResponse{
		MediaServerListJSONResponse: MediaServerListJSONResponse{
			Items: items,
		},
	}, nil
}

func (s *Server) GetMediaServer(
	ctx context.Context,
	request GetMediaServerRequestObject,
) (GetMediaServerResponseObject, error) {
	ms, ok := config.FindMediaServer(request.Name)
	if !ok {
		return GetMediaServer404JSONResponse{
			NotFoundJSONResponse: errNotFound("media server not found"),
		}, nil
	}
	return GetMediaServer200JSONResponse{
		MediaServerOKJSONResponse: MediaServerOKJSONResponse(mediaServerToAPI(ms)),
	}, nil
}

func (s *Server) CreateMediaServer(
	ctx context.Context,
	request CreateMediaServerRequestObject,
) (CreateMediaServerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateMediaServer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := config.MediaServerEntry{
		Name:           request.Body.Name,
		ServerType:     string(request.Body.ServerType),
		Host:           request.Body.Host,
		APIKey:         request.Body.ApiKey,
		LibrarySection: request.Body.LibrarySection,
	}
	if request.Body.Enabled != nil {
		e.Enabled = *request.Body.Enabled
	}
	if e.LibrarySection != nil && e.ServerType != "plex" {
		return CreateMediaServer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"library_section is only valid for Plex servers"),
		}, nil
	}

	switch err := config.AddMediaServer(ctx, e); {
	case errors.Is(err, config.ErrMediaServerExists):
		return CreateMediaServer409JSONResponse{
			ConflictJSONResponse: errConflict("media server name already exists"),
		}, nil
	case configLocked(err):
		return CreateMediaServer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateMediaServer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}

	return CreateMediaServer201JSONResponse{
		MediaServerCreatedJSONResponse: MediaServerCreatedJSONResponse(
			mediaServerToAPI(e),
		),
	}, nil
}

func (s *Server) UpdateMediaServer(
	ctx context.Context,
	request UpdateMediaServerRequestObject,
) (UpdateMediaServerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateMediaServer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	patch := config.MediaServerPatch{
		Host:           request.Body.Host,
		APIKey:         request.Body.ApiKey,
		Enabled:        request.Body.Enabled,
		LibrarySection: request.Body.LibrarySection,
	}
	if request.Body.ServerType != nil {
		st := string(*request.Body.ServerType)
		patch.ServerType = &st
	}

	switch err := config.UpdateMediaServer(ctx, request.Name, patch); {
	case errors.Is(err, config.ErrMediaServerNotFound):
		return UpdateMediaServer404JSONResponse{
			NotFoundJSONResponse: errNotFound("media server not found"),
		}, nil
	case configLocked(err):
		return UpdateMediaServer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateMediaServer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	ms, _ := config.FindMediaServer(request.Name)
	return UpdateMediaServer200JSONResponse{
		MediaServerOKJSONResponse: MediaServerOKJSONResponse(mediaServerToAPI(ms)),
	}, nil
}

func (s *Server) DeleteMediaServer(
	ctx context.Context,
	request DeleteMediaServerRequestObject,
) (DeleteMediaServerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteMediaServer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteMediaServer(ctx, request.Name); {
	case errors.Is(err, config.ErrMediaServerNotFound):
		return DeleteMediaServer404JSONResponse{
			NotFoundJSONResponse: errNotFound("media server not found"),
		}, nil
	case configLocked(err):
		return DeleteMediaServer403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteMediaServer500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
	return DeleteMediaServer204Response{}, nil
}

func (s *Server) TestMediaServer(
	ctx context.Context,
	request TestMediaServerRequestObject,
) (TestMediaServerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestMediaServer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := s.mediaServers.TestByName(ctx, request.Name)
	switch {
	case err == nil:
		return TestMediaServer200Response{}, nil
	case errors.Is(err, mediaserver.ErrServerNotFound):
		return TestMediaServer404JSONResponse{
			NotFoundJSONResponse: errNotFound("media server not found"),
		}, nil
	case errors.Is(err, mediaserver.ErrTestFailed):
		return TestMediaServer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestMediaServer500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
}

func (s *Server) TestDraftMediaServer(
	ctx context.Context,
	request TestDraftMediaServerRequestObject,
) (TestDraftMediaServerResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestDraftMediaServer403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	err := s.mediaServers.Test(ctx, mediaserver.TestParams{
		ServerType: string(request.Body.ServerType),
		Host:       request.Body.Host,
		APIKey:     request.Body.ApiKey,
	})
	switch {
	case err == nil:
		return TestDraftMediaServer200Response{}, nil
	case errors.Is(err, mediaserver.ErrInvalidServerType),
		errors.Is(err, mediaserver.ErrTestFailed):
		return TestDraftMediaServer422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	default:
		return TestDraftMediaServer500JSONResponse{
			InternalErrorJSONResponse: errInternal(err.Error()),
		}, nil
	}
}

func (s *Server) DiscoverMediaServerSections(
	ctx context.Context,
	request DiscoverMediaServerSectionsRequestObject,
) (DiscoverMediaServerSectionsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DiscoverMediaServerSections403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	sections, err := s.mediaServers.DiscoverSections(ctx, mediaserver.TestParams{
		ServerType: string(request.Body.ServerType),
		Host:       request.Body.Host,
		APIKey:     request.Body.ApiKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, mediaserver.ErrInvalidServerType):
			return DiscoverMediaServerSections422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		default:
			return DiscoverMediaServerSections500JSONResponse{
				InternalErrorJSONResponse: errInternal(err.Error()),
			}, nil
		}
	}

	out := make([]MediaServerSection, 0, len(sections))
	for _, sec := range sections {
		out = append(out, MediaServerSection{
			Key:       sec.Key,
			Name:      sec.Name,
			Type:      sec.Type,
			Locations: sec.Locations,
		})
	}

	return DiscoverMediaServerSections200JSONResponse{
		MediaServerDiscoveredJSONResponse: MediaServerDiscoveredJSONResponse{
			Sections: out,
		},
	}, nil
}
