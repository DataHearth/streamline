package restapi

import (
	"context"
	"net/http"
)

// customFormatsNotImplemented satisfies every custom-formats
// StrictServerInterface response type by writing a bare 501. Handlers land
// in Task 8; this stub exists only so the strict-server interface — which
// the OpenAPI spec now requires — compiles.
type customFormatsNotImplemented struct{}

func (customFormatsNotImplemented) VisitListCustomFormatsResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (customFormatsNotImplemented) VisitCreateCustomFormatResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (customFormatsNotImplemented) VisitGetCustomFormatResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (customFormatsNotImplemented) VisitUpdateCustomFormatResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (customFormatsNotImplemented) VisitDeleteCustomFormatResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (customFormatsNotImplemented) VisitTestCustomFormatResponse(
	w http.ResponseWriter,
) error {
	w.WriteHeader(http.StatusNotImplemented)
	return nil
}

func (s *Server) ListCustomFormats(
	context.Context, ListCustomFormatsRequestObject,
) (ListCustomFormatsResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}

func (s *Server) CreateCustomFormat(
	context.Context, CreateCustomFormatRequestObject,
) (CreateCustomFormatResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}

func (s *Server) GetCustomFormat(
	context.Context, GetCustomFormatRequestObject,
) (GetCustomFormatResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}

func (s *Server) UpdateCustomFormat(
	context.Context, UpdateCustomFormatRequestObject,
) (UpdateCustomFormatResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}

func (s *Server) DeleteCustomFormat(
	context.Context, DeleteCustomFormatRequestObject,
) (DeleteCustomFormatResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}

func (s *Server) TestCustomFormat(
	context.Context, TestCustomFormatRequestObject,
) (TestCustomFormatResponseObject, error) {
	return customFormatsNotImplemented{}, nil
}
