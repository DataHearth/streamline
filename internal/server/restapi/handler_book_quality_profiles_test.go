package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"Handler: BookQualityProfiles",
	Label("unit", "server", "books"),
	func() {
		var app *apiKeyApp

		const base = "/api/v1/books/quality-profiles"

		profileJSON := func(name string) string {
			return `{"name":"` + name + `","upgrade_allowed":true,` +
				`"ebook":{"formats":["AZW3","EPUB"],"preferred":"EPUB"},` +
				`"audiobook":{"formats":["MP3","M4B"],"preferred":"M4B","min_bitrate":64}}`
		}

		BeforeEach(func() {
			configtest.SetupFile(map[string]any{
				"book_quality_profiles": []map[string]any{
					{
						"name": "retail",
						"ebook": map[string]any{
							"formats": []string{"EPUB"}, "preferred": "EPUB",
						},
						"audiobook": map[string]any{
							"formats": []string{"M4B"}, "preferred": "M4B",
						},
					},
					{
						"name": "comics",
						"ebook": map[string]any{
							"formats": []string{"CBZ", "CBR"}, "preferred": "CBZ",
						},
						"audiobook": map[string]any{
							"formats": []string{"MP3"}, "preferred": "MP3",
						},
					},
				},
				"book_quality_default_profiles": map[string]any{
					"novel": "retail",
					"bd":    "comics",
					"comic": "comics",
					"manga": "retail",
				},
			})
			app = newAPIKeyApp()
		})

		send := func(method, path, key, body string) *http.Response {
			GinkgoHelper()
			var req *http.Request
			if body == "" {
				req = app.req(method, path, key, nil)
			} else {
				req = app.req(method, path, key, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
			}
			return app.do(req)
		}

		decode := func(resp *http.Response, out any) {
			GinkgoHelper()
			defer resp.Body.Close()
			Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
		}

		It("lists the profiles with the kinds each is the default for", func() {
			resp := send(http.MethodGet, base, app.requestOnlyKey, "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got []BookQualityProfile
			decode(resp, &got)
			Expect(got).To(HaveLen(2))
			Expect(got[0].DefaultFor).To(Equal([]BookKind{"novel", "manga"}))
			Expect(got[1].DefaultFor).To(Equal([]BookKind{"bd", "comic"}))
		})

		It("creates a profile, storing both slots best first", func() {
			resp := send(
				http.MethodPost,
				base,
				app.adminKey,
				profileJSON("both"),
			)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got BookQualityProfile
			decode(resp, &got)
			Expect(got.Ebook.Formats).To(Equal([]EbookFormat{"EPUB", "AZW3"}))
			Expect(got.Audiobook.Formats).To(Equal([]AudiobookFormat{"M4B", "MP3"}))
			Expect(got.Audiobook.MinBitrate).To(Equal(uint16(64)))
			Expect(got.DefaultFor).To(BeEmpty())
		})

		It("answers 409 for a name already taken", func() {
			resp := send(
				http.MethodPost,
				base,
				app.adminKey,
				profileJSON("retail"),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 422 when a preferred format is not ticked", func() {
			resp := send(http.MethodPost, base, app.adminKey,
				`{"name":"odd","upgrade_allowed":false,`+
					`"ebook":{"formats":["EPUB"],"preferred":"PDF"},`+
					`"audiobook":{"formats":["M4B"],"preferred":"M4B","min_bitrate":0}}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("answers 422 for a min_bitrate above 1024", func() {
			resp := send(
				http.MethodPost,
				base,
				app.adminKey,
				strings.Replace(
					profileJSON("loud"),
					`"min_bitrate":64`,
					`"min_bitrate":2000`,
					1,
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("answers 403 to a non-admin writing", func() {
			resp := send(
				http.MethodPost,
				base,
				app.requestOnlyKey,
				profileJSON("x"),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("replaces a profile on PUT", func() {
			resp := send(
				http.MethodPut,
				base+"/comics",
				app.adminKey,
				profileJSON("ignored"),
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got BookQualityProfile
			decode(resp, &got)
			Expect(got.Name).To(Equal("comics"))
			Expect(got.Ebook.Preferred).To(Equal(EbookFormat("EPUB")))
			Expect(got.DefaultFor).To(Equal([]BookKind{"bd", "comic"}))
		})

		It("answers 404 updating an unknown profile", func() {
			resp := send(
				http.MethodPut,
				base+"/ghost",
				app.adminKey,
				profileJSON("ghost"),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It(
			"answers 409 deleting a profile that is the default for any kind",
			func() {
				resp := send(http.MethodDelete, base+"/comics", app.adminKey, "")
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			},
		)

		It("sets one kind's default and then frees the old holder", func() {
			resp := send(
				http.MethodPost,
				base+"/retail/default?kind=bd",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			resp = send(
				http.MethodPost,
				base+"/retail/default?kind=comic",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			resp = send(http.MethodDelete, base+"/comics", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("answers 404 setting an unknown profile as default", func() {
			resp := send(
				http.MethodPost,
				base+"/ghost/default?kind=bd",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 404 for a missing profile on delete", func() {
			resp := send(http.MethodDelete, base+"/ghost", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	},
)
