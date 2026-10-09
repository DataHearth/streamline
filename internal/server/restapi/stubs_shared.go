package restapi

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("not implemented")

func (s *Server) ApproveRequest(
	_ context.Context,
	_ ApproveRequestRequestObject,
) (ApproveRequestResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) CreateBookQualityProfile(
	_ context.Context,
	_ CreateBookQualityProfileRequestObject,
) (CreateBookQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) CreateMusicQualityProfile(
	_ context.Context,
	_ CreateMusicQualityProfileRequestObject,
) (CreateMusicQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) CreateRequest(
	_ context.Context,
	_ CreateRequestRequestObject,
) (CreateRequestResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) DeleteBookQualityProfile(
	_ context.Context,
	_ DeleteBookQualityProfileRequestObject,
) (DeleteBookQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) DenyRequest(
	_ context.Context,
	_ DenyRequestRequestObject,
) (DenyRequestResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) GetRequestMetadata(
	_ context.Context,
	_ GetRequestMetadataRequestObject,
) (GetRequestMetadataResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) ListBookQualityProfiles(
	_ context.Context,
	_ ListBookQualityProfilesRequestObject,
) (ListBookQualityProfilesResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) ListMusicQualityProfiles(
	_ context.Context,
	_ ListMusicQualityProfilesRequestObject,
) (ListMusicQualityProfilesResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) ListRequests(
	_ context.Context,
	_ ListRequestsRequestObject,
) (ListRequestsResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) ReopenRequest(
	_ context.Context,
	_ ReopenRequestRequestObject,
) (ReopenRequestResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) SetDefaultBookQualityProfile(
	_ context.Context,
	_ SetDefaultBookQualityProfileRequestObject,
) (SetDefaultBookQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) SetDefaultMusicQualityProfile(
	_ context.Context,
	_ SetDefaultMusicQualityProfileRequestObject,
) (SetDefaultMusicQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) UpdateBookQualityProfile(
	_ context.Context,
	_ UpdateBookQualityProfileRequestObject,
) (UpdateBookQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}

func (s *Server) UpdateMusicQualityProfile(
	_ context.Context,
	_ UpdateMusicQualityProfileRequestObject,
) (UpdateMusicQualityProfileResponseObject, error) {
	return nil, errNotImplemented
}
