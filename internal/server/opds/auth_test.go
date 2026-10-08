package opds

import (
	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("auth", g.Label("unit"), func() {
	g.It("accepts matching Basic credentials", func() {
		Expect(checkToken("sesame", "sesame")).To(BeTrue())
	})
	g.It("rejects a wrong or empty stored token", func() {
		Expect(checkToken("sesame", "nope")).To(BeFalse())
		Expect(checkToken("", "")).To(BeFalse())
	})
})
