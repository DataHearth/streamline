package events

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("download record attribution", Label("unit", "events"), func() {
	It(
		"adds the ctx's record to a payload without changing the caller's map",
		func() {
			ctx := WithDownloadRecord(context.Background(), 42)
			in := map[string]any{"release_title": "X"}

			out := withDownloadID(ctx, in)

			Expect(out).To(HaveKeyWithValue(PayloadDownloadID, uint32(42)))
			Expect(out).To(HaveKeyWithValue("release_title", "X"))
			Expect(in).NotTo(HaveKey(PayloadDownloadID))
		},
	)

	It("gives a nil payload one", func() {
		ctx := WithDownloadRecord(context.Background(), 7)
		Expect(
			withDownloadID(ctx, nil),
		).To(Equal(map[string]any{PayloadDownloadID: uint32(7)}))
	})

	It("keeps a record the caller named", func() {
		ctx := WithDownloadRecord(context.Background(), 7)
		in := map[string]any{PayloadDownloadID: uint32(9)}
		Expect(
			withDownloadID(ctx, in),
		).To(HaveKeyWithValue(PayloadDownloadID, uint32(9)))
	})

	It("leaves the payload alone outside a record", func() {
		Expect(withDownloadID(context.Background(), nil)).To(BeNil())
		Expect(
			WithDownloadRecord(context.Background(), 0),
		).To(Equal(context.Background()))
	})

	It("reads the id back from a payload decoded from JSON", func() {
		Expect(
			DownloadID(map[string]any{PayloadDownloadID: float64(12)}),
		).To(Equal(uint32(12)))
		Expect(
			DownloadID(map[string]any{PayloadDownloadID: float64(1.5)}),
		).To(BeZero())
		Expect(
			DownloadID(map[string]any{PayloadDownloadID: float64(-3)}),
		).To(BeZero())
		Expect(DownloadID(map[string]any{PayloadDownloadID: "12"})).To(BeZero())
		Expect(DownloadID(nil)).To(BeZero())
	})
})
