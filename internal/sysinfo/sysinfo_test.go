package sysinfo

import (
	"math"

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
		Entry("a branch build injects the branch name", "main", "main"),
		Entry("a branch whose name starts with v", "v2-rewrite", "v2-rewrite"),
		Entry("a sha-tagged build", "sha-ae1acfa", "sha-ae1acfa"),
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
		// The badge strings must clamp too, not just the percentage —
		// otherwise they render a nonsense negative byte figure, or a Free
		// larger than the Total sitting next to it.
		Expect(du.Used).To(Equal("0 B"))
		Expect(du.Free).To(Equal("1000 B"))
		Expect(du.FreeBytes).To(Equal(int64(1000)))
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

		// A tiny total *and* a nonsense free at once: a filesystem
		// reporting an unsigned Bavail that wrapped int64 hands us a
		// negative free. Clamping free to 0 keeps used at the total, so the
		// honest answer is "full" and total/100 == 0 is never divided by.
		du = diskUsage(10, -math.MaxInt64/50)
		Expect(du).NotTo(BeNil())
		Expect(du.Pct).To(Equal(uint8(100)))
		Expect(du.FreeBytes).To(BeZero())
	})
})
