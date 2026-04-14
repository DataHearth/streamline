package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var _ = Describe("RegisterEntityMetrics", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		reader *metric.ManualReader
		mp     *metric.MeterProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		reader = metric.NewManualReader()
		mp = metric.NewMeterProvider(metric.WithReader(reader))
		DeferCleanup(func() { Expect(mp.Shutdown(ctx)).To(Succeed()) })
	})

	It("registers a movie gauge whose callback returns the current count", func() {
		Expect(
			RegisterEntityMetrics(mp.Meter("test"), client),
		).To(Succeed())

		_, err := client.Movie.Create().
			SetTmdbID(1).
			SetTitle("Matrix").
			SetOriginalTitle("Matrix").
			SetYear(1999).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		var rm metricdata.ResourceMetrics
		Expect(reader.Collect(ctx, &rm)).To(Succeed())

		value := gaugeValue(rm, "streamline_movies_total")
		Expect(value).To(Equal(int64(1)))
	})
})

func gaugeValue(rm metricdata.ResourceMetrics, name string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				return -1
			}
			if len(g.DataPoints) == 0 {
				return -1
			}
			return g.DataPoints[0].Value
		}
	}
	return -1
}
