package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/extension"
)

var (
	errNoExtensionCard = errors.New("no running extension has that card")
	errSessionRequired = errors.New("session authentication required")
)

// cardState is what an extension's credential card answers on GET.
type cardState struct {
	Set        bool       `json:"set"`
	CreatedAt  *time.Time `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	LastClient *string    `json:"last_client"`
}

func sessionClaims(ctx context.Context) (*auth.Claims, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil || claims.UserID == 0 {
		return nil, errors.New("unauthorized")
	}
	if claims.JTI == "" {
		return nil, errSessionRequired
	}
	return claims, nil
}

func (s *Server) runningCard(name string) *extension.AccountCard {
	for _, st := range s.extensions.Statuses() {
		if st.Name == name && st.State == extension.StateRunning &&
			st.Manifest != nil {
			return credentialCard(*st.Manifest)
		}
	}
	return nil
}

// callCard sends one request to an extension's card as the caller and returns
// the body of a 2xx answer.
func (s *Server) callCard(
	ctx context.Context,
	name string,
	card *extension.AccountCard,
	method string,
	claims *auth.Claims,
) ([]byte, error) {
	resp, found, err := s.extensions.Call(ctx, name, extension.Request{
		Method: method,
		Path:   "/cards/" + card.ID,
		Caller: &extension.Caller{ID: claims.UserID, Role: claims.Role},
	})
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, errNoExtensionCard
	case resp.Status >= http.StatusMultipleChoices:
		return nil, fmt.Errorf("the card answered status %d", resp.Status)
	}
	return resp.Body, nil
}

func (s *Server) cardAccess(
	ctx context.Context,
	name string,
	card *extension.AccountCard,
	claims *auth.Claims,
) (AppAccess, error) {
	body, err := s.callCard(ctx, name, card, http.MethodGet, claims)
	if err != nil {
		return AppAccess{}, err
	}
	var st cardState
	if err := json.Unmarshal(body, &st); err != nil {
		return AppAccess{}, fmt.Errorf("read the card's answer: %w", err)
	}
	out := AppAccess{
		Enabled:    st.Set,
		CreatedAt:  st.CreatedAt,
		LastUsedAt: st.LastUsedAt,
		LastClient: st.LastClient,
	}
	if card.Username == "email" {
		out.Username = claims.Email
	}
	return out, nil
}

func cardFailed(ctx context.Context, name string, err error) BadGatewayJSONResponse {
	slog.WarnContext(ctx, "Extension card did not answer",
		"extension", name, "error", err)
	return BadGatewayJSONResponse{Message: "the extension did not answer"}
}

func (s *Server) ListAccountExtensions(
	ctx context.Context,
	_ ListAccountExtensionsRequestObject,
) (ListAccountExtensionsResponseObject, error) {
	claims, err := sessionClaims(ctx)
	if err != nil {
		return ListAccountExtensions401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(err.Error()),
		}, nil
	}
	out := ListAccountExtensions200JSONResponse{}
	for _, st := range s.extensions.Statuses() {
		if st.State != extension.StateRunning || st.Manifest == nil {
			continue
		}
		card := credentialCard(*st.Manifest)
		if card == nil {
			continue
		}
		access, err := s.cardAccess(ctx, st.Name, card, claims)
		if err != nil {
			slog.WarnContext(ctx, "Extension card did not answer",
				"extension", st.Name, "error", err)
			continue
		}
		out = append(out, ExtensionAccountCard{
			Extension: st.Name,
			Kind:      card.Kind,
			Title:     card.Title,
			Mount:     card.Mount,
			Username:  card.Username,
			Enabled:   access.Enabled,
		})
	}
	return out, nil
}

func (s *Server) GetExtensionCredential(
	ctx context.Context,
	request GetExtensionCredentialRequestObject,
) (GetExtensionCredentialResponseObject, error) {
	claims, err := sessionClaims(ctx)
	if err != nil {
		return GetExtensionCredential401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(err.Error()),
		}, nil
	}
	card := s.runningCard(request.Name)
	if card == nil {
		return GetExtensionCredential404JSONResponse{
			NotFoundJSONResponse: errNotFound(errNoExtensionCard.Error()),
		}, nil
	}
	access, err := s.cardAccess(ctx, request.Name, card, claims)
	if err != nil {
		return GetExtensionCredential502JSONResponse{
			BadGatewayJSONResponse: cardFailed(ctx, request.Name, err),
		}, nil
	}
	return GetExtensionCredential200JSONResponse(access), nil
}

func (s *Server) RotateExtensionCredential(
	ctx context.Context,
	request RotateExtensionCredentialRequestObject,
) (RotateExtensionCredentialResponseObject, error) {
	claims, err := sessionClaims(ctx)
	if err != nil {
		return RotateExtensionCredential401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(err.Error()),
		}, nil
	}
	card := s.runningCard(request.Name)
	if card == nil {
		return RotateExtensionCredential404JSONResponse{
			NotFoundJSONResponse: errNotFound(errNoExtensionCard.Error()),
		}, nil
	}
	fail := func(err error) (RotateExtensionCredentialResponseObject, error) {
		return RotateExtensionCredential502JSONResponse{
			BadGatewayJSONResponse: cardFailed(ctx, request.Name, err),
		}, nil
	}
	body, err := s.callCard(ctx, request.Name, card, http.MethodPost, claims)
	if err != nil {
		return fail(err)
	}
	var created struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Secret == "" {
		return fail(errors.New("the card answered no secret"))
	}
	access, err := s.cardAccess(ctx, request.Name, card, claims)
	if err != nil {
		return fail(err)
	}
	return RotateExtensionCredential200JSONResponse(
		toAppAccessCreated(access, created.Secret),
	), nil
}

func (s *Server) RevokeExtensionCredential(
	ctx context.Context,
	request RevokeExtensionCredentialRequestObject,
) (RevokeExtensionCredentialResponseObject, error) {
	claims, err := sessionClaims(ctx)
	if err != nil {
		return RevokeExtensionCredential401JSONResponse{
			UnauthorizedJSONResponse: unauthorizedResp(err.Error()),
		}, nil
	}
	card := s.runningCard(request.Name)
	if card == nil {
		return RevokeExtensionCredential404JSONResponse{
			NotFoundJSONResponse: errNotFound(errNoExtensionCard.Error()),
		}, nil
	}
	_, err = s.callCard(ctx, request.Name, card, http.MethodDelete, claims)
	if err != nil {
		return RevokeExtensionCredential502JSONResponse{
			BadGatewayJSONResponse: cardFailed(ctx, request.Name, err),
		}, nil
	}
	return RevokeExtensionCredential204Response{}, nil
}

func toAppAccessCreated(a AppAccess, secret string) AppAccessCreated {
	return AppAccessCreated{
		Enabled:    a.Enabled,
		Username:   a.Username,
		CreatedAt:  a.CreatedAt,
		LastUsedAt: a.LastUsedAt,
		LastClient: a.LastClient,
		Secret:     secret,
	}
}
