package providerid

import (
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("provider ids", Label("unit"), func() {
	It("formats zero as no id and round-trips the rest", func() {
		Expect(Format(0)).To(BeEmpty())
		Expect(Format(603)).To(Equal("603"))
		n, ok := Parse("603")
		Expect(ok).To(BeTrue())
		Expect(n).To(Equal(uint32(603)))
	})

	DescribeTable("Parse refuses what names nothing",
		func(id string) {
			_, ok := Parse(id)
			Expect(ok).To(BeFalse())
		},
		Entry("empty", ""),
		Entry("zero", "0"),
		Entry("negative", "-1"),
		Entry("not a number", "abc"),
		Entry("a path", ".."),
		Entry("past 32 bits", "4294967296"),
	)

	DescribeTable("Valid takes the canonical form only",
		func(id string, want bool) { Expect(Valid(id)).To(Equal(want)) },
		Entry("canonical", "603", true),
		Entry("the largest", "4294967295", true),
		// Compared as strings, each of these would pass a duplicate check
		// against "603" and be stored beside it.
		Entry("a leading zero", "0603", false),
		Entry("a sign", "+603", false),
		Entry("a space", "603 ", false),
		Entry("zero", "0", false),
		Entry("empty", "", false),
	)

	It("orders ids as their numbers, not as strings", func() {
		ids := []string{"10", "9", "100", "11"}
		slices.SortFunc(ids, Compare)
		Expect(ids).To(Equal([]string{"9", "10", "11", "100"}))
		Expect(Compare("603", "603")).To(BeZero())
	})
})
