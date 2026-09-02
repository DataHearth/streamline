package langcode_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/langcode"
)

var _ = Describe("Canonical", Label("unit", "quality"), func() {
	It("folds the bibliographic code onto the terminological one", func() {
		// The spelling a real 5425-file library actually writes: 5292 "fre"
		// tags and not one "fra". A condition written either way has to land
		// on the same value or it matches nothing at all.
		Expect(langcode.Canonical("fre")).To(Equal("fra"))
		Expect(langcode.Canonical("ger")).To(Equal("deu"))
		Expect(langcode.Canonical("chi")).To(Equal("zho"))
	})

	It("folds two-letter codes and normalises case and padding", func() {
		Expect(langcode.Canonical("FR")).To(Equal("fra"))
		Expect(langcode.Canonical("  ja  ")).To(Equal("jpn"))
	})

	It("leaves an already-canonical or unknown code alone", func() {
		Expect(langcode.Canonical("fra")).To(Equal("fra"))
		Expect(langcode.Canonical("jpn")).To(Equal("jpn"))
		Expect(langcode.Canonical("mul")).To(Equal("mul"))
		Expect(langcode.Canonical("XYZ")).To(Equal("xyz"))
	})

	It("returns empty for empty", func() {
		Expect(langcode.Canonical("")).To(BeEmpty())
		Expect(langcode.Canonical("   ")).To(BeEmpty())
	})
})
