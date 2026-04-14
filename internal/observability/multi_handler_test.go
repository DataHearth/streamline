package observability

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// spyHandler captures every Handle call so tests can assert fan-out + ordering
// of WithAttrs/WithGroup propagation.
type spyHandler struct {
	mu        sync.Mutex
	records   []slog.Record
	attrs     []slog.Attr
	group     string
	minLevel  slog.Level
	handleErr error
}

func (s *spyHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= s.minLevel
}

func (s *spyHandler) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return s.handleErr
}

func (s *spyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := &spyHandler{
		minLevel:  s.minLevel,
		handleErr: s.handleErr,
		group:     s.group,
		attrs:     append(append([]slog.Attr{}, s.attrs...), attrs...),
	}
	return out
}

func (s *spyHandler) WithGroup(name string) slog.Handler {
	out := &spyHandler{
		minLevel:  s.minLevel,
		handleErr: s.handleErr,
		attrs:     append([]slog.Attr{}, s.attrs...),
		group:     name,
	}
	return out
}

var _ = Describe("multiHandler", Label("unit", "observability"), func() {
	var (
		ctx    context.Context
		a, b   *spyHandler
		h      slog.Handler
		record slog.Record
	)

	BeforeEach(func() {
		ctx = context.Background()
		a = &spyHandler{minLevel: slog.LevelInfo}
		b = &spyHandler{minLevel: slog.LevelInfo}
		h = multiHandler{a, b}
		record = slog.NewRecord(time.Time{}, slog.LevelInfo, "msg", 0)
	})

	Describe("Enabled", func() {
		It("returns true when any inner handler is enabled", func() {
			a.minLevel = slog.LevelError
			b.minLevel = slog.LevelDebug
			Expect(h.Enabled(ctx, slog.LevelInfo)).To(BeTrue())
		})

		It("returns false when no inner handler is enabled", func() {
			a.minLevel = slog.LevelError
			b.minLevel = slog.LevelError
			Expect(h.Enabled(ctx, slog.LevelInfo)).To(BeFalse())
		})

		It("returns false on an empty multiHandler", func() {
			Expect(multiHandler{}.Enabled(ctx, slog.LevelInfo)).To(BeFalse())
		})
	})

	Describe("Handle", func() {
		It("fans records out to every enabled inner handler", func() {
			Expect(h.Handle(ctx, record)).To(Succeed())
			Expect(a.records).To(HaveLen(1))
			Expect(b.records).To(HaveLen(1))
		})

		It(
			"skips handlers whose Enabled returns false for the record level",
			func() {
				b.minLevel = slog.LevelError
				Expect(h.Handle(ctx, record)).To(Succeed())
				Expect(a.records).To(HaveLen(1))
				Expect(b.records).To(BeEmpty())
			},
		)

		It("joins errors from inner handlers into a single error", func() {
			aErr := errors.New("a-fail")
			bErr := errors.New("b-fail")
			a.handleErr = aErr
			b.handleErr = bErr

			err := h.Handle(ctx, record)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, aErr)).To(BeTrue())
			Expect(errors.Is(err, bErr)).To(BeTrue())
		})
	})

	Describe("WithAttrs", func() {
		It("returns a new multiHandler whose inners inherit the attrs", func() {
			attrs := []slog.Attr{slog.String("k", "v")}
			out := h.WithAttrs(attrs).(multiHandler)
			Expect(out).To(HaveLen(2))
			Expect(out[0].(*spyHandler).attrs).To(Equal(attrs))
			Expect(out[1].(*spyHandler).attrs).To(Equal(attrs))
		})
	})

	Describe("WithGroup", func() {
		It("returns a new multiHandler whose inners inherit the group", func() {
			out := h.WithGroup("req").(multiHandler)
			Expect(out).To(HaveLen(2))
			Expect(out[0].(*spyHandler).group).To(Equal("req"))
			Expect(out[1].(*spyHandler).group).To(Equal("req"))
		})
	})
})
