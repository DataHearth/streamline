package jobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(
	r *http.Request,
) (*http.Response, error) {
	return f(r)
}

var _ = Describe("CardigannUpdate", Label("unit"), func() {
	It("stops at the commit it already runs, fetching nothing more", func() {
		configtest.Setup(map[string]any{"data_dir": GinkgoT().TempDir()})
		emb, err := definitions.Snapshot("")
		Expect(err).NotTo(HaveOccurred())
		var hosts []string
		orig := otelx.HTTPClient.Transport
		otelx.HTTPClient.Transport = roundTripFunc(
			func(r *http.Request) (*http.Response, error) {
				hosts = append(hosts, r.URL.Host)
				body := fmt.Sprintf(`{"sha":%q,"commit":{"committer":{"date":%q}}}`,
					emb.Upstream.Commit, emb.Upstream.Date)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		)
		DeferCleanup(func() { otelx.HTTPClient.Transport = orig })

		Expect(CardigannUpdate()(context.Background())).To(Succeed())
		Expect(hosts).To(Equal([]string{"api.github.com"}))
	})
})
