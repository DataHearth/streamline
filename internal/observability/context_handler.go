package observability

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// contextEnrichingHandler wraps another slog.Handler and pulls request-scoped
// attributes out of ctx on every Handle call, so every log record carries
// request_id, user_id, user_email, and http.route without callers having to
// pass them explicitly. Trace/span IDs are handled separately by the
// otelslog bridge inside the wrapped handler.
//
// Sits OUTSIDE multiHandler so every destination (stderr + OTel bridge) sees
// the same enriched attrs.
type contextEnrichingHandler struct {
	inner slog.Handler
}

// NewContextEnrichingHandler wraps h so that every record emitted through it
// gets request-scoped attributes pulled from ctx: request_id, user_id,
// user_email, http.route. Attrs are added only when ctx carries the source;
// records from non-HTTP code paths remain unchanged.
func NewContextEnrichingHandler(h slog.Handler) slog.Handler {
	return &contextEnrichingHandler{inner: h}
}

func (h *contextEnrichingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *contextEnrichingHandler) Handle(ctx context.Context, r slog.Record) error {
	// request_id has no OTel semconv (chi-specific); keep bespoke key.
	if id := chimw.GetReqID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	// OTel semconv v1.40 defines user.id / user.email / user.roles. Note:
	// user.roles is a string slice — we wrap our single role in a one-element
	// slice so the attribute matches the spec.
	if claims := auth.ClaimsFromContext(ctx); claims != nil && claims.UserID != 0 {
		r.AddAttrs(
			slog.Uint64(string(semconv.UserIDKey), uint64(claims.UserID)),
			slog.String(string(semconv.UserEmailKey), claims.Email),
			slog.Any(string(semconv.UserRolesKey), []string{claims.Role}),
		)
	}
	if rctx := chi.RouteContext(ctx); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			r.AddAttrs(slog.String(string(semconv.HTTPRouteKey), p))
		}
	}
	return h.inner.Handle(ctx, r)
}

func (h *contextEnrichingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextEnrichingHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *contextEnrichingHandler) WithGroup(name string) slog.Handler {
	return &contextEnrichingHandler{inner: h.inner.WithGroup(name)}
}
