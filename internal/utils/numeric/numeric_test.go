package numeric

import (
	"math"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

var _ = Describe("SaturateU16", Label("unit"), func() {
	It("passes an in-range value through", func() {
		Expect(SaturateU16(0)).To(Equal(uint16(0)))
		Expect(SaturateU16(99)).To(Equal(uint16(99)))
		Expect(SaturateU16(math.MaxUint16 - 1)).To(Equal(uint16(math.MaxUint16 - 1)))
	})

	It("clamps a negative to zero", func() {
		Expect(SaturateU16(-1)).To(Equal(uint16(0)))
		Expect(SaturateU16(math.MinInt64)).To(Equal(uint16(0)))
	})

	It("saturates at the ceiling instead of wrapping", func() {
		// The wrap this prevents: a 65 536-file folder stored as 0.
		Expect(SaturateU16(math.MaxUint16)).To(Equal(uint16(math.MaxUint16)))
		Expect(SaturateU16(math.MaxUint16 + 1)).To(Equal(uint16(math.MaxUint16)))
		Expect(SaturateU16(70000)).To(Equal(uint16(math.MaxUint16)))
	})

	It("narrows from an unsigned source too", func() {
		Expect(SaturateU16(uint(42))).To(Equal(uint16(42)))
		Expect(SaturateU16(uint64(math.MaxUint64))).To(Equal(uint16(math.MaxUint16)))
	})
})

var _ = Describe("SaturateU32", Label("unit"), func() {
	It("passes an in-range value through", func() {
		Expect(SaturateU32(0)).To(Equal(uint32(0)))
		Expect(SaturateU32(1234)).To(Equal(uint32(1234)))
	})

	It("clamps a negative to zero", func() {
		Expect(SaturateU32(-7)).To(Equal(uint32(0)))
	})

	It("saturates at the ceiling instead of wrapping", func() {
		Expect(SaturateU32(int64(math.MaxUint32))).To(Equal(uint32(math.MaxUint32)))
		Expect(
			SaturateU32(int64(math.MaxUint32) + 1),
		).To(Equal(uint32(math.MaxUint32)))
	})

	It("narrows from an unsigned source too", func() {
		Expect(SaturateU32(uint(9))).To(Equal(uint32(9)))
		Expect(SaturateU32(uint64(math.MaxUint64))).To(Equal(uint32(math.MaxUint32)))
	})
})

func TestNumeric(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Numeric Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})
