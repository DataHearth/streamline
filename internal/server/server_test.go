package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Server", Label("unit", "server"), func() {
	var (
		srv *Server
		ts  *httptest.Server
	)

	BeforeEach(func() {
		srv = New(Config{})
		ts = httptest.NewServer(srv.Router())
		DeferCleanup(func() { ts.Close() })
	})

	Describe("GET /health", func() {
		It("should return healthy status without auth", func() {
			resp, err := http.Get(ts.URL + "/health")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]string
			err = json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body["status"]).To(Equal("healthy"))
		})
	})
})
