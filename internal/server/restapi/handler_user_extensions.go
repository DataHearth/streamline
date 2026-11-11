package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/extension"
)

const codeCardValuesInvalid = "card_values_invalid"

func errCardValues(err error) UnprocessableEntityJSONResponse {
	code := codeCardValuesInvalid
	return UnprocessableEntityJSONResponse{Message: err.Error(), Code: &code}
}

var errNoSettingsCard = errors.New("no running extension has that settings card")

// callSettingsCard sends one request to a settings card on behalf of the
// calling admin, aimed at the target user.
func (s *Server) callSettingsCard(
	ctx context.Context,
	name, cardID, method string,
	target uint32,
	body []byte,
) ([]byte, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, errors.New("unauthorized")
	}
	resp, found, err := s.extensions.Call(ctx, name, extension.Request{
		Method:     method,
		Path:       "/cards/" + cardID,
		Body:       body,
		Caller:     &extension.Caller{ID: claims.UserID, Role: claims.Role},
		TargetUser: target,
	})
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, errNoSettingsCard
	case resp.Status >= http.StatusMultipleChoices:
		return nil, fmt.Errorf("the card answered status %d", resp.Status)
	}
	return resp.Body, nil
}

func (s *Server) settingsCard(name, id string) (extension.AccountCard, bool) {
	for _, st := range s.extensions.Statuses() {
		if st.Name != name || st.State != extension.StateRunning ||
			st.Manifest == nil {
			continue
		}
		for _, c := range settingsCards(*st.Manifest) {
			if c.ID == id {
				return c, true
			}
		}
	}
	return extension.AccountCard{}, false
}

func (s *Server) ListUserExtensions(
	ctx context.Context,
	req ListUserExtensionsRequestObject,
) (ListUserExtensionsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListUserExtensions403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if _, err := s.auth.GetUserByID(
		ctx,
		req.Uid,
	); errors.Is(
		err,
		auth.ErrUserNotFound,
	) {
		return ListUserExtensions404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	} else if err != nil {
		return nil, err
	}

	out := ListUserExtensions200JSONResponse{}
	for _, st := range s.extensions.Statuses() {
		if st.State != extension.StateRunning || st.Manifest == nil {
			continue
		}
		for _, c := range settingsCards(*st.Manifest) {
			body, err := s.callSettingsCard(
				ctx, st.Name, c.ID, http.MethodGet, req.Uid, nil,
			)
			var answer struct {
				Values map[string]any `json:"values"`
			}
			if err == nil {
				err = json.Unmarshal(body, &answer)
			}
			if err != nil {
				slog.WarnContext(ctx, "Extension settings card did not answer",
					"extension", st.Name, "card", c.ID, "error", err)
				continue
			}
			out = append(out, UserExtensionCard{
				Extension: st.Name,
				Card:      c.ID,
				Title:     c.Title,
				Fields:    cardFieldsToAPI(c.Fields),
				Values:    answer.Values,
			})
		}
	}
	return out, nil
}

func (s *Server) UpdateUserExtensionCard(
	ctx context.Context,
	req UpdateUserExtensionCardRequestObject,
) (UpdateUserExtensionCardResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateUserExtensionCard403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if _, err := s.auth.GetUserByID(
		ctx,
		req.Uid,
	); errors.Is(
		err,
		auth.ErrUserNotFound,
	) {
		return UpdateUserExtensionCard404JSONResponse{
			NotFoundJSONResponse: notFoundResp("user not found"),
		}, nil
	} else if err != nil {
		return nil, err
	}
	card, ok := s.settingsCard(req.Name, req.Card)
	if !ok {
		return UpdateUserExtensionCard404JSONResponse{
			NotFoundJSONResponse: errNotFound(errNoSettingsCard.Error()),
		}, nil
	}
	body, err := json.Marshal(req.Body)
	if err != nil {
		return nil, err
	}
	if err := card.CheckValues(body); err != nil {
		return UpdateUserExtensionCard422JSONResponse{
			UnprocessableEntityJSONResponse: errCardValues(err),
		}, nil
	}
	if _, err := s.callSettingsCard(
		ctx, req.Name, req.Card, http.MethodPut, req.Uid, body,
	); err != nil {
		if errors.Is(err, errNoSettingsCard) {
			return UpdateUserExtensionCard404JSONResponse{
				NotFoundJSONResponse: errNotFound(err.Error()),
			}, nil
		}
		return UpdateUserExtensionCard502JSONResponse{
			BadGatewayJSONResponse: cardFailed(ctx, req.Name, err),
		}, nil
	}
	return UpdateUserExtensionCard204Response{}, nil
}
