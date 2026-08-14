package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// oversizeJSON builds a syntactically valid JSON object whose single string
// value pushes the payload past the 1 MiB ceiling, so nothing but the size can
// be the reason a request is refused.
func oversizeJSON() []byte {
	return []byte(`{"name":"` + strings.Repeat("a", (1<<20)+4096) + `"}`)
}

// rawPost sends a prebuilt body with a declared Content-Length.
func rawPost(
	url string,
	body []byte,
	headers map[string]string,
) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := clientNoRedirect().Do(req)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(resp.Body.Close)
	return resp
}

// chunkedPost sends the same body with no declared length. An io.Reader Go
// cannot size makes the transport chunk the request, which is the shape the
// Content-Length check cannot catch and MaxBytesReader has to.
func chunkedPost(
	url string,
	body []byte,
	headers map[string]string,
) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(
		http.MethodPost, url, io.NopCloser(bytes.NewReader(body)),
	)
	Expect(err).ToNot(HaveOccurred())
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := clientNoRedirect().Do(req)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(resp.Body.Close)
	return resp
}

func decodeErrorBody(resp *http.Response) map[string]string {
	GinkgoHelper()
	var body map[string]string
	Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
	return body
}

var _ = Describe("Request body limits", Label("integration", "server"), func() {
	var app *testApp

	const (
		seedEmail = "limits-admin@x.com"
		seedPw    = "limits-Passw0rd!"
	)

	BeforeEach(func() {
		app = newWebAuthTestApp(appOpts{
			regMode:      "disabled",
			seedEmail:    seedEmail,
			seedPassword: seedPw,
		})
		DeferCleanup(app.close)
	})

	Context("pre-auth web routes", func() {
		sameOrigin := map[string]string{"Sec-Fetch-Site": "same-origin"}

		It("refuses a declared oversize login, leaving the limiter alone", func() {
			resp := rawPost(
				app.httpSrv.URL+"/auth/login", oversizeJSON(), sameOrigin,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(
				resp.Header.Get("Content-Type"),
			).To(HavePrefix("application/json"))
			Expect(decodeErrorBody(resp)).To(
				HaveKeyWithValue("message", "request body too large"),
			)

			// The reject happens before the handler, so no attempt was charged
			// and the very next valid login still succeeds.
			Expect(loginAsSeedAdmin(app, seedEmail, seedPw)).ToNot(BeEmpty())
		})

		It("refuses a chunked oversize /auth/login with body_too_large", func() {
			resp := chunkedPost(
				app.httpSrv.URL+"/auth/login", oversizeJSON(), sameOrigin,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(decodeErrorBody(resp)).To(
				HaveKeyWithValue("code", "body_too_large"),
			)
		})
	})

	Context("authenticated /api/v1", func() {
		var bearer map[string]string

		BeforeEach(func() {
			tok := loginAsSeedAdmin(app, seedEmail, seedPw)
			bearer = map[string]string{"Authorization": "Bearer " + tok}
		})

		It("refuses a declared oversize body with 413 JSON", func() {
			resp := rawPost(
				app.httpSrv.URL+"/api/v1/quality-profiles",
				oversizeJSON(),
				bearer,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(decodeErrorBody(resp)).To(
				HaveKeyWithValue("message", "request body too large"),
			)
		})

		It("refuses a chunked oversize body with 413 JSON", func() {
			resp := chunkedPost(
				app.httpSrv.URL+"/api/v1/quality-profiles",
				oversizeJSON(),
				bearer,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(decodeErrorBody(resp)).To(
				HaveKeyWithValue("message", "request body too large"),
			)
		})

		It("still accepts a normally-sized body", func() {
			body, err := json.Marshal(map[string]any{
				"name":                 "body-limit-control",
				"preferred_resolution": "1080p",
			})
			Expect(err).ToNot(HaveOccurred())

			resp := rawPost(
				app.httpSrv.URL+"/api/v1/quality-profiles", body, bearer,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})
	})

	// Pins the chain position: the limit sits behind auth, so an anonymous
	// caller learns nothing about it and the anonymous route surface is
	// provably the one that shipped.
	It("answers an anonymous oversize /api/v1 POST with 401, not 413", func() {
		resp := rawPost(
			app.httpSrv.URL+"/api/v1/quality-profiles", oversizeJSON(), nil,
		)

		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).ToNot(ContainSubstring("too large"))
	})
})
