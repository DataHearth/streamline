package observability

import (
	"bytes"
	"context"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Setup stderr handler", Label("unit", "observability"), func() {
	setup := func(level, format string, w io.Writer) *slog.Logger {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"log": map[string]any{
				"app": map[string]any{
					"level":  level,
					"format": format,
				},
			},
		})
		handler, shutdown, err := Setup(
			context.Background(),
			Config{
				ServiceName:  "test",
				StderrWriter: w,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(shutdown(context.Background())).To(Succeed()) })
		return slog.New(handler)
	}

	Context("when configured for development", func() {
		It("should emit text records to the provided writer", func() {
			var buf bytes.Buffer
			setup("debug", "text", &buf).
				InfoContext(context.Background(), "test message", "key", "value")

			Expect(buf.String()).To(ContainSubstring("test message"))
			Expect(buf.String()).To(ContainSubstring("key=value"))
		})
	})

	Context("when configured for production", func() {
		It("should emit JSON records", func() {
			var buf bytes.Buffer
			setup(
				"info",
				"json",
				&buf,
			).InfoContext(context.Background(), "test message")

			Expect(buf.String()).To(ContainSubstring(`"msg":"test message"`))
		})
	})

	Context("when level is set", func() {
		It("should filter messages below the configured level", func() {
			var buf bytes.Buffer
			logger := setup("warn", "text", &buf)
			logger.InfoContext(context.Background(), "should not appear")
			logger.WarnContext(context.Background(), "should appear")

			Expect(buf.String()).NotTo(ContainSubstring("should not appear"))
			Expect(buf.String()).To(ContainSubstring("should appear"))
		})
	})
})

var _ = Describe("OTel Setup", Label("unit", "observability"), func() {
	Context("when endpoint is empty", func() {
		It("should return stderr-only handler and no-op shutdown", func() {
			configtest.Setup(map[string]any{
				"log": map[string]any{
					"app": map[string]any{
						"level":  "info",
						"format": "text",
					},
				},
			})
			var buf bytes.Buffer
			handler, shutdown, err := Setup(
				context.Background(),
				Config{
					ServiceName:  "test",
					StderrWriter: &buf,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
			Expect(shutdown(context.Background())).To(Succeed())

			slog.New(handler).InfoContext(context.Background(), "hello")
			Expect(buf.String()).To(ContainSubstring("hello"))
		})
	})
})
