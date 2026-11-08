package restapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/update"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

// errCatalogUnavailable is a catalog that could not be read from upstream.
var errCatalogUnavailable = errors.New(
	"the definition catalog could not be read from Prowlarr/Indexers",
)

// catalogVersion keeps the catalog's version of file under the data
// directory — fetching upstream first when nothing has been read since the
// server started — and names the commit it was read at.
func catalogVersion(ctx context.Context, file string) (string, error) {
	if err := update.Ensure(ctx, otelx.HTTPClient); err != nil {
		return "", fmt.Errorf("%w: %w", errCatalogUnavailable, err)
	}
	set := definitions.Current()
	b, err := set.Raw(file)
	if err != nil {
		return "", err
	}
	commit := set.Upstream().Commit
	return commit, definitions.Keep(config.Get().DataDir, file, commit, b)
}

// pinCardigann pins a cardigann entry to the catalog's version of the
// definition it names.
func pinCardigann(ctx context.Context, e *config.IndexerEntry) error {
	if e.Protocol != "cardigann" || e.Cardigann == nil ||
		e.Cardigann.Definition == "" {
		return nil
	}
	commit, err := catalogVersion(ctx, e.Cardigann.Definition)
	if err != nil {
		return err
	}
	e.Cardigann.Commit = commit
	return nil
}

// pinnedVersion is the version a stored cardigann entry is pinned to,
// fetched at its commit when it is not kept yet.
func pinnedVersion(ctx context.Context, e config.IndexerEntry) ([]byte, error) {
	b, err := definitions.Resolve(
		ctx, otelx.HTTPClient, config.Get().DataDir,
		e.Cardigann.Definition, e.Cardigann.Commit,
	)
	if err != nil && !errors.Is(err, definitions.ErrNotFound) {
		return nil, fmt.Errorf("%w: %w", errCatalogUnavailable, err)
	}
	return b, err
}

func summaryToAPI(s cardigann.Summary) CardigannSummary {
	nonNil := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	return CardigannSummary{
		File: s.File, Name: s.Name, Description: s.Description,
		Language: s.Language, Type: s.Type, Links: nonNil(s.Links),
		Login: s.Login, Captcha: s.Captcha, Challenge: s.Challenge,
		Categories: nonNil(s.Categories),
	}
}

func (s *Server) GetCardigannCatalog(
	ctx context.Context,
	_ GetCardigannCatalogRequestObject,
) (GetCardigannCatalogResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetCardigannCatalog403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if err := update.Ensure(ctx, otelx.HTTPClient); err != nil {
		return GetCardigannCatalog503JSONResponse{
			ServiceUnavailableJSONResponse: errCatalogUnavailableResp(
				fmt.Errorf("%w: %w", errCatalogUnavailable, err).Error(),
			),
		}, nil
	}
	cat, err := definitions.Catalog()
	if err != nil {
		return GetCardigannCatalog500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	out := CardigannCatalog{
		Upstream: CardigannUpstream{
			Commit: cat.Upstream.Commit,
			Date:   cat.Upstream.Date,
		},
		Definitions: make([]CardigannSummary, 0, len(cat.Definitions)),
	}
	for _, d := range cat.Definitions {
		out.Definitions = append(out.Definitions, summaryToAPI(d))
	}
	return GetCardigannCatalog200JSONResponse(out), nil
}

func (s *Server) GetCardigannDefinition(
	ctx context.Context,
	request GetCardigannDefinitionRequestObject,
) (GetCardigannDefinitionResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetCardigannDefinition403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	var b []byte
	var err error
	if name := request.Params.Indexer; name != nil {
		e, ok := config.FindIndexer(*name)
		if !ok || e.Cardigann == nil || e.Cardigann.Definition != request.File {
			return GetCardigannDefinition404JSONResponse{
				NotFoundJSONResponse: errNotFound(
					"no such indexer runs that definition",
				),
			}, nil
		}
		b, err = pinnedVersion(ctx, e)
	} else if err = update.Ensure(ctx, otelx.HTTPClient); err != nil {
		err = fmt.Errorf("%w: %w", errCatalogUnavailable, err)
	} else {
		b, err = definitions.Current().Raw(request.File)
	}
	switch {
	case errors.Is(err, definitions.ErrNotFound):
		return GetCardigannDefinition404JSONResponse{
			NotFoundJSONResponse: errNotFound("definition not found"),
		}, nil
	case errors.Is(err, errCatalogUnavailable):
		return GetCardigannDefinition503JSONResponse{
			ServiceUnavailableJSONResponse: errCatalogUnavailableResp(
				err.Error(),
			),
		}, nil
	case err != nil:
		return GetCardigannDefinition500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	def, err := definitions.Decode(b)
	if err != nil {
		return GetCardigannDefinition500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	base := summaryToAPI(cardigann.Summarize(request.File, def))
	out := CardigannDefinition{
		File: base.File, Name: base.Name, Description: base.Description,
		Language: base.Language, Type: base.Type, Links: base.Links,
		Login: base.Login, Captcha: base.Captcha, Challenge: base.Challenge,
		Categories: base.Categories,
		Settings:   make([]CardigannSetting, 0, len(def.Settings)),
	}
	for _, st := range def.Settings {
		cs := CardigannSetting{
			Name: st.Name, Label: st.Label, Type: st.Type, Secret: st.Secret,
		}
		if cs.Label == "" {
			cs.Label = st.Name
		}
		if st.Default != nil {
			d := string(*st.Default)
			// Info text is upstream's HTML; the SPA gets it as plain text
			// rather than markup it would have to trust.
			if strings.HasPrefix(st.Type, "info") {
				if doc, err := goquery.NewDocumentFromReader(
					strings.NewReader(d),
				); err == nil {
					d = strings.TrimSpace(doc.Text())
				}
			}
			cs.Default = &d
		}
		if len(st.Options) > 0 {
			opts := make([]CardigannOption, 0, len(st.Options))
			for _, o := range st.Options {
				opts = append(
					opts,
					CardigannOption{Key: o.Key, Label: string(o.Value)},
				)
			}
			cs.Options = &opts
		}
		out.Settings = append(out.Settings, cs)
	}
	return GetCardigannDefinition200JSONResponse(out), nil
}
