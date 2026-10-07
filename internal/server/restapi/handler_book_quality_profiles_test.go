package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type bookProfileFamily struct {
	name    string
	path    string
	cfgKey  string
	first   string
	second  string
	format  string
	other   string
	resolve func(string) (string, bool)
	listIs  func(*http.Response) []bool
}

var _ = Describe(
	"Handler: Book quality profiles",
	Label("unit", "server", "books"),
	func() {
		var app *apiKeyApp

		families := []bookProfileFamily{
			{
				name: "ebook", path: "/api/v1/books/ebook-quality-profiles",
				cfgKey: "ebook_quality", first: "epub-first", second: "portable",
				format: "epub", other: "azw3",
				resolve: func(n string) (string, bool) {
					p, ok := config.ResolveEbookQualityProfile(n)
					return p.Name, ok
				},
				listIs: func(r *http.Response) []bool {
					var items []EbookQualityProfile
					Expect(json.NewDecoder(r.Body).Decode(&items)).To(Succeed())
					out := make([]bool, len(items))
					for i, it := range items {
						out[i] = it.IsDefault
					}
					return out
				},
			},
			{
				name: "audiobook", path: "/api/v1/books/audiobook-quality-profiles",
				cfgKey: "audiobook_quality", first: "m4b-first", second: "portable",
				format: "m4b", other: "mp3",
				resolve: func(n string) (string, bool) {
					p, ok := config.ResolveAudiobookQualityProfile(n)
					return p.Name, ok
				},
				listIs: func(r *http.Response) []bool {
					var items []AudiobookQualityProfile
					Expect(json.NewDecoder(r.Body).Decode(&items)).To(Succeed())
					out := make([]bool, len(items))
					for i, it := range items {
						out[i] = it.IsDefault
					}
					return out
				},
			},
		}

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

		for _, fam := range families {
			Context(fam.name, func() {
				BeforeEach(func() {
					configtest.SetupFile(map[string]any{
						fam.cfgKey + "_profiles": []map[string]any{
							{
								"name":    fam.first,
								"formats": []string{fam.format, fam.other},
								"cutoff":  fam.format,
							},
							{
								"name":    fam.second,
								"formats": []string{fam.other},
								"cutoff":  fam.other,
							},
						},
						fam.cfgKey + "_default_profile": fam.first,
					})
					app = newAPIKeyApp()
				})

				It("lists the profiles and flags the default", func() {
					resp := send(http.MethodGet, fam.path, app.adminKey, "")
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
					Expect(fam.listIs(resp)).To(Equal([]bool{true, false}))
				})

				It("lets a request-only caller list", func() {
					resp := send(http.MethodGet, fam.path, app.requestOnlyKey, "")
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
				})

				It("creates a profile and persists it to config", func() {
					resp := send(
						http.MethodPost,
						fam.path,
						app.adminKey,
						`{"name":"mid","formats":["`+fam.format+`"],"cutoff":"`+fam.format+`","upgrade_allowed":true}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusCreated))

					name, ok := fam.resolve("mid")
					Expect(ok).To(BeTrue())
					Expect(name).To(Equal("mid"))
				})

				It("answers 409 for a duplicate name", func() {
					resp := send(
						http.MethodPost,
						fam.path,
						app.adminKey,
						`{"name":"`+fam.first+`","formats":["`+fam.format+`"],"cutoff":"`+fam.format+`","upgrade_allowed":false}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				})

				It("answers 422 for an unknown cutoff", func() {
					resp := send(
						http.MethodPost,
						fam.path,
						app.adminKey,
						`{"name":"bad","formats":["`+fam.format+`"],"cutoff":"wav","upgrade_allowed":false}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				})

				It("updates a profile by path name", func() {
					resp := send(
						http.MethodPut,
						fam.path+"/"+fam.second,
						app.adminKey,
						`{"name":"ignored","formats":["`+fam.other+`"],"cutoff":"`+fam.other+`","upgrade_allowed":true}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					name, ok := fam.resolve(fam.second)
					Expect(ok).To(BeTrue())
					Expect(name).To(Equal(fam.second))
				})

				It("404s when updating an unknown profile", func() {
					resp := send(
						http.MethodPut,
						fam.path+"/ghost",
						app.adminKey,
						`{"name":"ghost","formats":["`+fam.format+`"],"cutoff":"`+fam.format+`","upgrade_allowed":false}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				})

				It("deletes a non-default profile", func() {
					resp := send(
						http.MethodDelete,
						fam.path+"/"+fam.second,
						app.adminKey,
						"",
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
				})

				It("answers 409 when deleting the default profile", func() {
					resp := send(
						http.MethodDelete,
						fam.path+"/"+fam.first,
						app.adminKey,
						"",
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				})

				It("404s when deleting an unknown profile", func() {
					resp := send(
						http.MethodDelete,
						fam.path+"/ghost",
						app.adminKey,
						"",
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				})

				It("answers 403 to a request-only caller writing a profile", func() {
					resp := send(
						http.MethodPost,
						fam.path,
						app.requestOnlyKey,
						`{"name":"mid","formats":["`+fam.format+`"],"cutoff":"`+fam.format+`","upgrade_allowed":false}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				})

				It("answers 403 to a member writing a profile", func() {
					app.addMember("")
					resp := send(
						http.MethodPost,
						fam.path,
						app.memberKey,
						`{"name":"mid","formats":["`+fam.format+`"],"cutoff":"`+fam.format+`","upgrade_allowed":false}`,
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
				})
			})
		}
	},
)
