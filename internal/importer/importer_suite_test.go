package importer

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/datahearth/streamline/internal/testutil"
)

// metricReader collects the importer's metrics for the whole suite. The
// package's instruments are created at init on the global delegate, which
// binds them to the first MeterProvider ever installed, so the suite installs
// exactly one and specs compare counts before and after what they run.
var metricReader *sdkmetric.ManualReader

func TestImporter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Importer Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
	metricReader = sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	otel.SetMeterProvider(mp)
	DeferCleanup(func(ctx context.Context) {
		Expect(mp.Shutdown(ctx)).To(Succeed())
	})
})

// outcomeCounts reads the running totals of streamline.importer.outcomes,
// keyed by outcome.
func outcomeCounts() map[string]int64 {
	GinkgoHelper()
	var rm metricdata.ResourceMetrics
	Expect(metricReader.Collect(context.Background(), &rm)).To(Succeed())
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "streamline.importer.outcomes" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			Expect(ok).To(BeTrue(), "importer outcomes is not an int64 sum")
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key("outcome"))
				counts[v.AsString()] += dp.Value
			}
		}
	}
	return counts
}

// outcomeDelta is what each outcome moved by since before.
func outcomeDelta(before map[string]int64) map[string]int64 {
	GinkgoHelper()
	delta := map[string]int64{}
	for k, v := range outcomeCounts() {
		if d := v - before[k]; d != 0 {
			delta[k] = d
		}
	}
	return delta
}
