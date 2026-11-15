package middleware

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("CORS middleware", Label("unit", "server"), func() {
	const origin = "https://player.example.com"

	var reached bool

	call := func(method, from string, preflight bool) *httptest.ResponseRecorder {
		GinkgoHelper()
		reached = false
		h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusUnauthorized)
		}))
		r := httptest.NewRequest(method, "/api/v1/movies", nil)
		if from != "" {
			r.Header.Set("Origin", from)
		}
		if preflight {
			r.Header.Set("Access-Control-Request-Method", http.MethodPost)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	Context("with an origin listed", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"server": map[string]any{"cors_origins": []string{origin}},
			})
		})

		It("answers its preflight without reaching auth", func() {
			w := call(http.MethodOptions, origin, true)
			Expect(reached).To(BeFalse())
			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(Equal(origin))
			Expect(w.Header().Get("Access-Control-Allow-Headers")).
				To(ContainSubstring("X-API-Key"))
			Expect(w.Header().Get("Access-Control-Allow-Credentials")).To(BeEmpty())
		})

		It("lets it read an answer, errors included", func() {
			w := call(http.MethodGet, origin, false)
			Expect(reached).To(BeTrue())
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(Equal(origin))
			Expect(w.Header().Get("Vary")).To(Equal("Origin"))
		})

		It("gives another origin nothing", func() {
			w := call(http.MethodOptions, "https://evil.example.com", true)
			Expect(reached).To(BeTrue())
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		})
	})

	It("does nothing when no origin is listed", func() {
		configtest.Setup()
		w := call(http.MethodOptions, origin, true)
		Expect(reached).To(BeTrue())
		Expect(w.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		Expect(w.Header().Get("Vary")).To(BeEmpty())
	})
})
