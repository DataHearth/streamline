package events

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Type.Valid", Label("unit", "events"), func() {
	It("accepts all canonical types", func() {
		for _, t := range []Type{
			TypeGrabbed, TypeDownloadCompleted, TypeDownloadFailed,
			TypeImported, TypeImportFailed,
			TypeDriftDetected, TypeDriftConfirmed,
		} {
			Expect(t.Valid()).To(BeTrue(), "type %q should be valid", t)
		}
	})

	It("rejects unknown types", func() {
		Expect(Type("nope").Valid()).To(BeFalse())
		Expect(Type("").Valid()).To(BeFalse())
	})
})
