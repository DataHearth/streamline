package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/extension"
)

const codeExtensionUnreadable = "extension_unreadable"

func errExtensionUnreadable(err error) UnprocessableEntityJSONResponse {
	code := codeExtensionUnreadable
	return UnprocessableEntityJSONResponse{Message: err.Error(), Code: &code}
}

func (s *Server) extensionView(name string) Extension {
	for _, e := range config.Get().Extensions {
		if e.Name == name {
			return extensionToAPI(e, s.extensions.Statuses())
		}
	}
	return Extension{}
}

func (s *Server) ListExtensions(
	ctx context.Context,
	_ ListExtensionsRequestObject,
) (ListExtensionsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListExtensions403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	entries := config.Get().Extensions
	statuses := s.extensions.Statuses()
	items := make([]Extension, 0, len(entries))
	for _, e := range entries {
		items = append(items, extensionToAPI(e, statuses))
	}
	return ListExtensions200JSONResponse{Items: items}, nil
}

func (s *Server) InspectExtension(
	ctx context.Context,
	request InspectExtensionRequestObject,
) (InspectExtensionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return InspectExtension403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := config.ExtensionEntry{Source: request.Body.Source}
	if request.Body.Sha256 != nil {
		e.SHA256 = *request.Body.Sha256
	}
	mf, err := s.extensions.Inspect(ctx, e)
	if err != nil {
		return InspectExtension422JSONResponse{
			UnprocessableEntityJSONResponse: errExtensionUnreadable(err),
		}, nil
	}
	return InspectExtension200JSONResponse{
		Name:      mf.Name,
		Version:   mf.Version,
		Requested: permissionsToAPI(mf.Grants(), &mf),
		Card:      cardToAPI(mf),
	}, nil
}

func (s *Server) CreateExtension(
	ctx context.Context,
	request CreateExtensionRequestObject,
) (CreateExtensionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateExtension403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	b := request.Body
	e := config.ExtensionEntry{Name: b.Name, Source: b.Source}
	if b.Sha256 != nil {
		e.SHA256 = *b.Sha256
	}
	if b.Enabled != nil {
		e.Enabled = *b.Enabled
	}
	if b.Grants != nil {
		e.Grants = grantsToConfig(*b.Grants)
	}

	switch err := config.AddExtension(ctx, e); {
	case errors.Is(err, config.ErrExtensionExists):
		return CreateExtension409JSONResponse{
			ConflictJSONResponse: errConflict("extension name already exists"),
		}, nil
	case configLocked(err):
		return CreateExtension403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateExtension422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if err := s.extensions.Reconcile(ctx); err != nil {
		return CreateExtension500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	v := s.extensionView(e.Name)
	return CreateExtension201JSONResponse{
		ExtensionCreatedJSONResponse: ExtensionCreatedJSONResponse(v),
	}, nil
}

func (s *Server) UpdateExtension(
	ctx context.Context,
	request UpdateExtensionRequestObject,
) (UpdateExtensionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateExtension403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	b := request.Body
	err := config.UpdateExtension(
		ctx,
		request.Name,
		func(e *config.ExtensionEntry) error {
			if b.Source != nil {
				e.Source = *b.Source
			}
			if b.Sha256 != nil {
				e.SHA256 = *b.Sha256
			}
			if b.Enabled != nil {
				e.Enabled = *b.Enabled
			}
			if b.Grants != nil {
				e.Grants = grantsToConfig(*b.Grants)
			}
			return nil
		},
	)
	switch {
	case errors.Is(err, config.ErrExtensionNotFound):
		return UpdateExtension404JSONResponse{
			NotFoundJSONResponse: errNotFound("extension not found"),
		}, nil
	case configLocked(err):
		return UpdateExtension403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateExtension422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if err := s.extensions.Reconcile(ctx); err != nil {
		return UpdateExtension500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	v := s.extensionView(request.Name)
	return UpdateExtension200JSONResponse{
		ExtensionOKJSONResponse: ExtensionOKJSONResponse(v),
	}, nil
}

func (s *Server) ApproveExtension(
	ctx context.Context,
	request ApproveExtensionRequestObject,
) (ApproveExtensionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ApproveExtension403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	var entry config.ExtensionEntry
	found := false
	for _, e := range config.Get().Extensions {
		if e.Name == request.Name {
			entry, found = e, true
		}
	}
	if !found {
		return ApproveExtension404JSONResponse{
			NotFoundJSONResponse: errNotFound("extension not found"),
		}, nil
	}
	mf, err := s.extensions.Inspect(ctx, entry)
	if err != nil {
		return ApproveExtension422JSONResponse{
			UnprocessableEntityJSONResponse: errExtensionUnreadable(err),
		}, nil
	}

	err = config.UpdateExtension(
		ctx,
		request.Name,
		func(e *config.ExtensionEntry) error {
			e.Grants = mf.Grants()
			return nil
		},
	)
	switch {
	case errors.Is(err, config.ErrExtensionNotFound):
		return ApproveExtension404JSONResponse{
			NotFoundJSONResponse: errNotFound("extension not found"),
		}, nil
	case configLocked(err):
		return ApproveExtension403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return ApproveExtension422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if err := s.extensions.Reconcile(ctx); err != nil {
		return ApproveExtension500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	v := s.extensionView(request.Name)
	return ApproveExtension200JSONResponse{
		ExtensionOKJSONResponse: ExtensionOKJSONResponse(v),
	}, nil
}

func (s *Server) DeleteExtension(
	ctx context.Context,
	request DeleteExtensionRequestObject,
) (DeleteExtensionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteExtension403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteExtension(ctx, request.Name); {
	case errors.Is(err, config.ErrExtensionNotFound):
		return DeleteExtension404JSONResponse{
			NotFoundJSONResponse: errNotFound("extension not found"),
		}, nil
	case configLocked(err):
		return DeleteExtension403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteExtension500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	if err := s.extensions.Reconcile(ctx); err != nil {
		return DeleteExtension500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	if request.Params.Purge != nil && *request.Params.Purge {
		if err := s.extensions.Purge(request.Name); err != nil {
			return DeleteExtension500JSONResponse{
				InternalErrorJSONResponse: errInternal(ctx, err),
			}, nil
		}
	}
	return DeleteExtension204Response{}, nil
}

func grantsToConfig(g ExtensionGrants) config.ExtensionGrants {
	out := config.ExtensionGrants{
		Mounts: g.Mounts, Views: g.Views, Services: g.Services,
	}
	if g.Folders != nil {
		out.Folders = *g.Folders
	}
	return out
}

// permissionsToAPI spells grants out for a person: each mount with how the
// module authenticates it, each view with what granting it lets a module do.
// mf is nil when the module could not be read.
func permissionsToAPI(
	g config.ExtensionGrants,
	mf *extension.Manifest,
) ExtensionPermissions {
	out := ExtensionPermissions{
		Mounts:   make([]ExtensionMount, 0, len(g.Mounts)),
		Views:    make([]ExtensionViewInfo, 0, len(g.Views)),
		Services: nonNil(g.Services),
		Folders:  nonNil(g.Folders),
	}
	for _, p := range g.Mounts {
		m := ExtensionMount{Path: p}
		if mf != nil {
			for _, mt := range mf.Mounts {
				if mt.Prefix == p {
					m.Auth = &mt.Auth
				}
			}
		}
		out.Mounts = append(out.Mounts, m)
	}
	for _, name := range g.Views {
		v, _ := db.ExtensionViewByName(name)
		info := ExtensionViewInfo{Name: name, Description: v.Description}
		if v.Bundle != "" {
			info.Bundle = &v.Bundle
		}
		out.Views = append(out.Views, info)
	}
	return out
}

// credentialCard is the one account card core draws for an extension.
func credentialCard(mf extension.Manifest) *extension.AccountCard {
	for _, c := range mf.AccountCards {
		if c.Kind == extension.CardCredential {
			return &c
		}
	}
	return nil
}

// settingsCards are every settings card an extension declares.
func settingsCards(mf extension.Manifest) []extension.AccountCard {
	var out []extension.AccountCard
	for _, c := range mf.AccountCards {
		if c.Kind == extension.CardSettings {
			out = append(out, c)
		}
	}
	return out
}

func cardFieldsToAPI(fields []extension.CardField) []ExtensionCardField {
	out := make([]ExtensionCardField, 0, len(fields))
	for _, f := range fields {
		out = append(out, ExtensionCardField{
			Id:    f.ID,
			Kind:  ExtensionCardFieldKind(f.Kind),
			Label: f.Label,
			Min:   f.Min,
			Max:   f.Max,
		})
	}
	return out
}

func cardToAPI(mf extension.Manifest) *ExtensionCard {
	c := credentialCard(mf)
	if c == nil {
		return nil
	}
	card := &ExtensionCard{
		Kind: c.Kind, Title: c.Title, Mount: c.Mount, Username: c.Username,
	}
	if len(c.Fields) > 0 {
		fields := cardFieldsToAPI(c.Fields)
		card.Fields = &fields
	}
	return card
}

func extensionToAPI(e config.ExtensionEntry, statuses []extension.Status) Extension {
	out := Extension{
		Name:    e.Name,
		Source:  e.Source,
		Enabled: e.Enabled,
		State:   ExtensionState("starting"),
		Granted: permissionsToAPI(e.Grants, nil),
	}
	if e.SHA256 != "" {
		out.Sha256 = &e.SHA256
	}
	for _, st := range statuses {
		if st.Name != e.Name {
			continue
		}
		out.State = ExtensionState(st.State)
		if st.Reason != "" {
			out.Reason = &st.Reason
		}
		if mf := st.Manifest; mf != nil {
			requested := permissionsToAPI(mf.Grants(), mf)
			out.Requested = &requested
			out.Granted = permissionsToAPI(e.Grants, mf)
			out.Card = cardToAPI(*mf)
			if mf.Version != "" {
				out.Version = &mf.Version
			}
		}
	}
	return out
}
