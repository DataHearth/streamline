package restapi

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
)

// customFormatConditionToAPI maps a compiled quality.Condition to the
// generated view. Used for both builtins (Format.Conditions carries an
// unexported compiled regex alongside the exported fields this reads) and
// user-defined entries, via customFormatConditionsFromEntries.
func customFormatConditionToAPI(c quality.Condition) CustomFormatCondition {
	out := CustomFormatCondition{Type: CustomFormatConditionType(c.Type)}
	if c.Pattern != "" {
		p := c.Pattern
		out.Pattern = &p
	}
	if c.Value != "" {
		v := c.Value
		out.Value = &v
	}
	if c.MinGB != 0 {
		v := c.MinGB
		out.MinGb = &v
	}
	if c.MaxGB != 0 {
		v := c.MaxGB
		out.MaxGb = &v
	}
	if c.Min != 0 {
		v := c.Min
		out.Min = &v
	}
	if c.Required {
		v := true
		out.Required = &v
	}
	if c.Negate {
		v := true
		out.Negate = &v
	}
	return out
}

// customFormatConditionsFromEntries converts stored config entries into
// quality.Condition without compiling anything (no regex, no validation) —
// listing/reading a format must not fail because a condition is malformed.
func customFormatConditionsFromEntries(
	cs []config.CustomFormatConditionEntry,
) []quality.Condition {
	out := make([]quality.Condition, len(cs))
	for i, c := range cs {
		out[i] = quality.Condition{
			Type:     quality.ConditionType(c.Type),
			Pattern:  c.Pattern,
			Value:    c.Value,
			MinGB:    c.MinGB,
			MaxGB:    c.MaxGB,
			Min:      c.Min,
			Required: c.Required,
			Negate:   c.Negate,
		}
	}
	return out
}

// customFormatToAPI maps a user-defined config entry, including its
// optional description when set.
func customFormatToAPI(e config.CustomFormatEntry) CustomFormat {
	return customFormatToAPIWithDescription(e, false, e.Description)
}

// customFormatToAPIWithDescription is customFormatToAPI plus the builtin
// flag and a description — a builtin's fixed one, or a user format's
// optional one. An empty description leaves CustomFormat.Description
// absent rather than "".
func customFormatToAPIWithDescription(
	e config.CustomFormatEntry, builtin bool, description string,
) CustomFormat {
	conds := customFormatConditionsFromEntries(e.Conditions)
	apiConds := make([]CustomFormatCondition, len(conds))
	for i, c := range conds {
		apiConds[i] = customFormatConditionToAPI(c)
	}
	out := CustomFormat{Name: e.Name, Conditions: apiConds}
	if builtin {
		b := true
		out.Builtin = &b
	}
	if description != "" {
		d := description
		out.Description = &d
	}
	return out
}

// builtinEntry re-shapes a quality.Format's exported fields into a
// config.CustomFormatEntry so builtins and user-defined formats share the
// same customFormatToAPI conversion path.
func builtinEntry(f quality.Format) config.CustomFormatEntry {
	conds := make([]config.CustomFormatConditionEntry, len(f.Conditions))
	for i, c := range f.Conditions {
		conds[i] = config.CustomFormatConditionEntry{
			Type:     string(c.Type),
			Pattern:  c.Pattern,
			Value:    c.Value,
			MinGB:    c.MinGB,
			MaxGB:    c.MaxGB,
			Min:      c.Min,
			Required: c.Required,
			Negate:   c.Negate,
		}
	}
	return config.CustomFormatEntry{Name: f.Name, Conditions: conds}
}

// customFormatFromAPI maps a request body (create/update share the same
// name+conditions shape as CustomFormat, modulo the read-only builtin field)
// into a config entry ready for ToFormat/AddCustomFormat/UpdateCustomFormat.
func customFormatFromAPI(b CustomFormat) config.CustomFormatEntry {
	var description string
	if b.Description != nil {
		description = *b.Description
	}
	conds := make([]config.CustomFormatConditionEntry, len(b.Conditions))
	for i, c := range b.Conditions {
		e := config.CustomFormatConditionEntry{Type: string(c.Type)}
		if c.Pattern != nil {
			e.Pattern = *c.Pattern
		}
		if c.Value != nil {
			e.Value = *c.Value
		}
		if c.MinGb != nil {
			e.MinGB = *c.MinGb
		}
		if c.MaxGb != nil {
			e.MaxGB = *c.MaxGb
		}
		if c.Min != nil {
			e.Min = *c.Min
		}
		if c.Required != nil {
			e.Required = *c.Required
		}
		if c.Negate != nil {
			e.Negate = *c.Negate
		}
		conds[i] = e
	}
	return config.CustomFormatEntry{
		Name: b.Name, Description: description, Conditions: conds,
	}
}

func (s *Server) ListCustomFormats(
	_ context.Context, _ ListCustomFormatsRequestObject,
) (ListCustomFormatsResponseObject, error) {
	builtins := quality.Builtins()
	items := make([]CustomFormat, 0, len(builtins))
	for _, f := range builtins {
		items = append(items, customFormatToAPIWithDescription(
			builtinEntry(f), true, f.Description,
		))
	}
	c := config.Get()
	for _, e := range c.CustomFormats {
		items = append(items, customFormatToAPI(e))
	}
	return ListCustomFormats200JSONResponse(items), nil
}

func (s *Server) GetCustomFormat(
	_ context.Context, request GetCustomFormatRequestObject,
) (GetCustomFormatResponseObject, error) {
	if f, ok := quality.BuiltinByName(request.Name); ok {
		return GetCustomFormat200JSONResponse(
			customFormatToAPIWithDescription(builtinEntry(f), true, f.Description),
		), nil
	}
	e, ok := config.FindCustomFormat(request.Name)
	if !ok {
		return GetCustomFormat404JSONResponse{
			NotFoundJSONResponse: errNotFound("custom format not found"),
		}, nil
	}
	return GetCustomFormat200JSONResponse(customFormatToAPI(e)), nil
}

func (s *Server) CreateCustomFormat(
	ctx context.Context, request CreateCustomFormatRequestObject,
) (CreateCustomFormatResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateCustomFormat403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := customFormatFromAPI(CustomFormat{
		Name:        request.Body.Name,
		Description: request.Body.Description,
		Conditions:  request.Body.Conditions,
	})
	// Compiled here rather than left to config's own invariant check so the
	// body carries the coded, condition-naming message the SPA renders
	// verbatim, instead of the same error wrapped in a config-key path.
	if _, err := e.ToFormat(); err != nil {
		return CreateCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errInvalidCondition(err.Error()),
		}, nil
	}

	switch err := config.AddCustomFormat(ctx, e); {
	case errors.Is(err, config.ErrCustomFormatExists),
		errors.Is(err, config.ErrCustomFormatBuiltin):
		return CreateCustomFormat409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return CreateCustomFormat403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return CreateCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return CreateCustomFormat201JSONResponse(customFormatToAPI(e)), nil
}

func (s *Server) UpdateCustomFormat(
	ctx context.Context, request UpdateCustomFormatRequestObject,
) (UpdateCustomFormatResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateCustomFormat403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	e := customFormatFromAPI(CustomFormat{
		Name:        request.Body.Name,
		Description: request.Body.Description,
		Conditions:  request.Body.Conditions,
	})
	if _, err := e.ToFormat(); err != nil {
		return UpdateCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errInvalidCondition(err.Error()),
		}, nil
	}

	switch err := config.UpdateCustomFormat(ctx, request.Name, e); {
	case errors.Is(err, config.ErrCustomFormatNotFound):
		return UpdateCustomFormat404JSONResponse{
			NotFoundJSONResponse: errNotFound("custom format not found"),
		}, nil
	case errors.Is(err, config.ErrCustomFormatExists),
		errors.Is(err, config.ErrCustomFormatBuiltin):
		return UpdateCustomFormat409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateCustomFormat403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return UpdateCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return UpdateCustomFormat200JSONResponse(customFormatToAPI(e)), nil
}

func (s *Server) DeleteCustomFormat(
	ctx context.Context, request DeleteCustomFormatRequestObject,
) (DeleteCustomFormatResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteCustomFormat403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteCustomFormat(ctx, request.Name); {
	case errors.Is(err, config.ErrCustomFormatNotFound):
		return DeleteCustomFormat404JSONResponse{
			NotFoundJSONResponse: errNotFound("custom format not found"),
		}, nil
	case errors.Is(err, config.ErrCustomFormatBuiltin),
		errors.Is(err, config.ErrCustomFormatInUse):
		return DeleteCustomFormat409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case configLocked(err):
		return DeleteCustomFormat403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return DeleteCustomFormat500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return DeleteCustomFormat204Response{}, nil
}

func (s *Server) TestCustomFormat(
	ctx context.Context, request TestCustomFormatRequestObject,
) (TestCustomFormatResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return TestCustomFormat403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	// quality.NewFormat no-ops on an empty slice — Matches then returns true
	// vacuously (no required conditions to fail, no optional one needed).
	// The spec declares conditions minItems: 1; enforce it here since
	// ToFormat alone would let it through as a false positive.
	if len(request.Body.Conditions) == 0 {
		return TestCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"conditions must not be empty",
			),
		}, nil
	}
	e := customFormatFromAPI(CustomFormat{Conditions: request.Body.Conditions})
	f, err := e.ToFormat()
	if err != nil {
		return TestCustomFormat422JSONResponse{
			UnprocessableEntityJSONResponse: errInvalidCondition(err.Error()),
		}, nil
	}

	sample := request.Body.Sample
	var size int64
	if sample.Size != nil {
		size = *sample.Size
	}
	var seeders uint32
	if sample.Seeders != nil {
		seeders = *sample.Seeders
	}
	episodes := 1
	if sample.Episodes != nil && *sample.Episodes > 0 {
		episodes = *sample.Episodes
	}
	rc := qualityctx.ContextFromRelease(sample.Title, size, seeders, episodes)

	explain := f.Explain(rc)
	conds := make([]CustomFormatConditionResult, len(explain))
	for i, passed := range explain {
		conds[i] = CustomFormatConditionResult{Index: i, Passed: passed}
	}
	return TestCustomFormat200JSONResponse(CustomFormatTestResult{
		Matched:    f.Matches(rc),
		Conditions: conds,
	}), nil
}
