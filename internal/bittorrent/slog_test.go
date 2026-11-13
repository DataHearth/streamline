package bittorrent

import (
	"bytes"
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("staleFlushHandler", Label("unit", "bittorrent"), func() {
	It("drops the two stale flush warnings and keeps the rest", func() {
		ctx := context.Background()
		var buf bytes.Buffer
		log := slog.New(staleFlushHandler{slog.NewTextHandler(&buf, nil)})

		log.WarnContext(ctx, "error flushing completed piece", "piece", 4982)
		log.WarnContext(
			ctx,
			"error flushing file before promotion",
			"file",
			"a.mkv.part",
		)
		log.With("k", "v").WarnContext(ctx, "error flushing completed piece")
		log.WarnContext(
			ctx,
			"file too small for piece marked as complete",
			"piece",
			3,
		)

		Expect(buf.String()).NotTo(ContainSubstring("error flushing"))
		Expect(buf.String()).To(ContainSubstring("file too small"))
	})
})
