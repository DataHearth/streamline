package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/media/book"
)

func (s *Server) SendBook(
	ctx context.Context,
	req SendBookRequestObject,
) (SendBookResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return SendBook403JSONResponse{
			ForbiddenJSONResponse{Message: "unauthorized"},
		}, nil
	}
	err := s.books.SendBook(ctx, claims.UserID, req.Id)
	switch {
	case err == nil:
		return SendBook204Response{}, nil
	case errors.Is(err, book.ErrBookNotFound):
		return SendBook404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrNotSendable):
		return SendBook422JSONResponse{
			UnprocessableEntityJSONResponse: errNotSendable(err),
		}, nil
	case errors.Is(err, book.ErrNoDeviceEmail):
		return SendBook422JSONResponse{
			UnprocessableEntityJSONResponse: errNoDeviceEmail(err),
		}, nil
	case errors.Is(err, book.ErrSMTPNotConfigured):
		return SendBook503JSONResponse{
			ServiceUnavailableJSONResponse: errSMTPNotConfigured(err),
		}, nil
	default:
		return SendBook502JSONResponse{
			BadGatewayJSONResponse: errSendFailed(err),
		}, nil
	}
}

func (s *Server) SendBookSeries(
	ctx context.Context,
	req SendBookSeriesRequestObject,
) (SendBookSeriesResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return SendBookSeries403JSONResponse{
			ForbiddenJSONResponse{Message: "unauthorized"},
		}, nil
	}
	plan, err := s.books.SendSeries(ctx, claims.UserID, req.Id)
	switch {
	case err == nil:
		skipped := make([]SeriesSendSkipped, len(plan.Skipped))
		for i, v := range plan.Skipped {
			skipped[i] = SeriesSendSkipped{
				BookId: v.BookID, Title: v.Title, Reason: v.Reason,
			}
		}
		return SendBookSeries202JSONResponse{
			SeriesSendPlanResponseJSONResponse{
				Emails:      plan.Emails,
				SentVolumes: plan.Volumes,
				Skipped:     skipped,
			},
		}, nil
	case errors.Is(err, book.ErrSeriesNotFound):
		return SendBookSeries404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case errors.Is(err, book.ErrNotSendable):
		return SendBookSeries422JSONResponse{
			UnprocessableEntityJSONResponse: errNotSendable(err),
		}, nil
	case errors.Is(err, book.ErrNoDeviceEmail):
		return SendBookSeries422JSONResponse{
			UnprocessableEntityJSONResponse: errNoDeviceEmail(err),
		}, nil
	case errors.Is(err, book.ErrSMTPNotConfigured):
		return SendBookSeries503JSONResponse{
			ServiceUnavailableJSONResponse: errSMTPNotConfigured(err),
		}, nil
	default:
		return SendBookSeries500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
}
