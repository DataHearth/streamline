package subsonic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("envelope", g.Label("unit"), func() {
	g.It("writes the XML envelope by default", func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/rest/ping", nil)
		writeOK(rec, req, nil)
		Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("text/xml"))
		Expect(rec.Body.String()).To(ContainSubstring(`<subsonic-response`))
		Expect(rec.Body.String()).To(ContainSubstring(`status="ok"`))
		Expect(rec.Body.String()).To(ContainSubstring(`version="1.16.1"`))
	})

	g.It("writes JSON when f=json", func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/rest/ping?f=json", nil)
		writeOK(rec, req, nil)
		var payload map[string]map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &payload)).To(Succeed())
		Expect(payload["subsonic-response"]["status"]).To(Equal("ok"))
	})

	g.It("writes spec error codes", func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/rest/ping?f=json", nil)
		writeError(rec, req, errWrongCredentials, "Wrong username or password")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring(`"code":40`))
	})
})
