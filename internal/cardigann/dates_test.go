package cardigann_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
)

var _ = Describe("ParseDate", Label("unit", "cardigann"), func() {
	now := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)

	It("tries each layout in order", func() {
		t, err := cardigann.ParseDate(
			[]string{"2006-01-02 15:04 -07:00", "2006-01-02 15:04 -0700"},
			"2024-03-05 09:07 +0100", now,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.UTC()).To(Equal(time.Date(2024, 3, 5, 8, 7, 0, 0, time.UTC)))
	})

	It("takes the year from now when the layout has none, as .NET does", func() {
		t, err := cardigann.ParseDate([]string{"01/02 15:04"}, "10/05 12:00", now)
		Expect(err).NotTo(HaveOccurred())
		Expect(t).To(Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)))
	})

	It("takes the whole date from now when the layout has no date", func() {
		t, err := cardigann.ParseDate([]string{"15:04"}, "12:34", now)
		Expect(err).NotTo(HaveOccurred())
		Expect(t).To(Equal(time.Date(2026, 10, 9, 12, 34, 0, 0, time.UTC)))
	})

	It("reads a zone-less layout in now's location", func() {
		paris, err := time.LoadLocation("Europe/Paris")
		if err != nil {
			Skip("no tzdata")
		}
		t, err := cardigann.ParseDate(
			[]string{"2006-01-02 15:04"}, "2024-03-05 09:07", now.In(paris),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Location()).To(Equal(paris))
	})

	It("fails when no layout accepts the value", func() {
		_, err := cardigann.ParseDate([]string{"2006-01-02"}, "yesterday", now)
		Expect(err).To(HaveOccurred())
	})
})
