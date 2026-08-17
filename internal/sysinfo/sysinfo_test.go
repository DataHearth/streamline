package sysinfo

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("displayVersion", Label("unit", "sysinfo"), func() {
	DescribeTable("renders one canonical version string",
		func(injected, want string) {
			Expect(displayVersion(injected)).To(Equal(want))
		},
		Entry("goreleaser injects a bare semver", "1.3.0", "v1.3.0"),
		Entry("the image build injects the tag", "v1.3.0", "v1.3.0"),
		Entry("a plain go build injects nothing", "", "dev"),
	)
})

var _ = Describe("diskUsage", Label("unit", "sysinfo"), func() {
	It("computes an ordinary percentage", func() {
		du := diskUsage(1000, 250)
		Expect(du).NotTo(BeNil())
		Expect(du.Pct).To(Equal(uint8(75)))
		Expect(du.Kind).To(Equal("warn"))
	})

	It("returns nil for a bogus total", func() {
		Expect(diskUsage(0, 0)).To(BeNil())
		Expect(diskUsage(-5, 0)).To(BeNil())
	})

	It("clamps negative used (root-reserved blocks) to 0%", func() {
		du := diskUsage(1000, 1200)
		Expect(du).NotTo(BeNil())
		Expect(du.Pct).To(Equal(uint8(0)))
		Expect(du.Kind).To(Equal("ok"))
	})

	It("survives volumes where used*100 would overflow int64", func() {
		// ~100 PB nearly full: used*100 wraps int64; the divide-first path
		// must still report ~99%, not a clamped 0%.
		total := int64(1e17)
		free := int64(1e15)
		du := diskUsage(total, free)
		Expect(du).NotTo(BeNil())
		Expect(du.Pct).To(Equal(uint8(99)))
		Expect(du.Kind).To(Equal("err"))
	})

	It("handles totals below 100 bytes without dividing by zero", func() {
		du := diskUsage(10, 5)
		Expect(du).NotTo(BeNil())
		Expect(du.Pct).To(Equal(uint8(50)))
	})
})
