package restapi

import (
	"context"
	"errors"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/config"
)

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
		Categories: nonNil(s.Categories), Custom: s.Custom,
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
	cat, err := definitions.Catalog(config.Get().CardigannDir())
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
	dir := config.Get().CardigannDir()
	def, err := definitions.Load(dir, request.File)
	switch {
	case errors.Is(err, definitions.ErrNotFound):
		return GetCardigannDefinition404JSONResponse{
			NotFoundJSONResponse: errNotFound("definition not found"),
		}, nil
	case err != nil:
		return GetCardigannDefinition500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	sum := cardigann.Summarize(request.File, def)
	cat, err := definitions.Catalog(dir)
	if err == nil {
		for _, c := range cat.Definitions {
			if c.File == request.File {
				sum.Custom = c.Custom
			}
		}
	}
	base := summaryToAPI(sum)
	out := CardigannDefinition{
		File: base.File, Name: base.Name, Description: base.Description,
		Language: base.Language, Type: base.Type, Links: base.Links,
		Login: base.Login, Captcha: base.Captcha, Challenge: base.Challenge,
		Categories: base.Categories, Custom: base.Custom,
		Settings: make([]CardigannSetting, 0, len(def.Settings)),
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
