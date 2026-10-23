package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("contextEnrichingHandler", Label("unit", "observability"), func() {
	var (
		spy    *spyHandler
		h      slog.Handler
		record slog.Record
	)

	BeforeEach(func() {
		spy = &spyHandler{minLevel: slog.LevelDebug}
		h = NewContextEnrichingHandler(spy)
		record = slog.NewRecord(time.Time{}, slog.LevelInfo, "msg", 0)
	})

	// recordAttrs collects the attrs from the first captured record.
	recordAttrs := func() map[string]slog.Value {
		GinkgoHelper()
		Expect(spy.records).To(HaveLen(1))
		out := map[string]slog.Value{}
		spy.records[0].Attrs(func(a slog.Attr) bool {
			out[a.Key] = a.Value
			return true
		})
		return out
	}

	Describe("Handle", func() {
		It("passes records through unchanged for bare contexts", func() {
			Expect(h.Handle(context.Background(), record)).To(Succeed())
			Expect(recordAttrs()).To(BeEmpty())
		})

		It("adds request_id when chi RequestID middleware ran", func() {
			ctx := context.WithValue(
				context.Background(),
				chimw.RequestIDKey,
				"req-123",
			)
			Expect(h.Handle(ctx, record)).To(Succeed())
			attrs := recordAttrs()
			Expect(attrs).To(HaveKeyWithValue("request_id",
				slog.StringValue("req-123")))
		})

		// user.id/email/roles enrichment exercised through auth middleware
		// integration tests; we can't synthesize claims here without
		// exporting auth.claimsKey.

		It("adds http.route when chi RouteContext carries a pattern", func() {
			rctx := chi.NewRouteContext()
			rctx.RoutePatterns = []string{"/api/v1/movies/{id}"}
			ctx := context.WithValue(
				context.Background(), chi.RouteCtxKey, rctx,
			)
			Expect(h.Handle(ctx, record)).To(Succeed())
			attrs := recordAttrs()
			Expect(attrs).To(HaveKeyWithValue("http.route",
				slog.StringValue("/api/v1/movies/{id}")))
		})

		It("propagates errors from the inner handler", func() {
			// swap in a failing spy; NewContextEnrichingHandler already wraps
			// the first spy, so build a fresh wrapper here.
			failing := &spyHandler{
				minLevel:  slog.LevelDebug,
				handleErr: errContextPropagate,
			}
			wrapped := NewContextEnrichingHandler(failing)
			Expect(wrapped.Handle(context.Background(), record)).
				To(MatchError(errContextPropagate))
		})
	})

	Describe("Enabled", func() {
		It("delegates to the inner handler", func() {
			spy.minLevel = slog.LevelError
			Expect(h.Enabled(context.Background(), slog.LevelInfo)).To(BeFalse())
			Expect(h.Enabled(context.Background(), slog.LevelError)).To(BeTrue())
		})
	})

	Describe("WithAttrs / WithGroup", func() {
		It("WithAttrs wraps the inner handler's WithAttrs result", func() {
			attrs := []slog.Attr{slog.String("k", "v")}
			out := h.WithAttrs(attrs).(*contextEnrichingHandler)
			Expect(out.inner.(*spyHandler).attrs).To(Equal(attrs))
		})

		It("WithGroup wraps the inner handler's WithGroup result", func() {
			out := h.WithGroup("grp").(*contextEnrichingHandler)
			Expect(out.inner.(*spyHandler).group).To(Equal("grp"))
		})
	})
})

var errContextPropagate = &handlerError{msg: "inner-fail"}

type handlerError struct{ msg string }

func (e *handlerError) Error() string { return e.msg }
