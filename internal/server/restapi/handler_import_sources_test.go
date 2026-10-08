package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	"github.com/datahearth/streamline/internal/arr"
	arrmocks "github.com/datahearth/streamline/internal/arr/mocks"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func postSourceJSON(app *apiKeyApp, path, key string, body any) *http.Response {
	GinkgoHelper()
	raw, err := json.Marshal(body)
	Expect(err).NotTo(HaveOccurred())
	req := app.req(http.MethodPost, path, key, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return app.do(req)
}

func decodeBody[T any](resp *http.Response) T {
	GinkgoHelper()
	var out T
	Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
	return out
}

// connected wires the factory mock to hand back a library that passes its
// connection test.
func connected(app *apiKeyApp, app2 arr.App) *arrmocks.MockLibrary {
	GinkgoHelper()
	lib := arrmocks.NewMockLibrary(GinkgoT())
	app.arrClients.EXPECT().
		Client(app2, "http://radarr.lan:7878", "k").
		Return(lib, nil).Once()
	lib.EXPECT().TestConnection(mock.Anything).Return(nil).Once()
	return lib
}

var arrSourceBody = map[string]any{
	"app": "radarr", "url": "http://radarr.lan:7878", "api_key": "k",
}

var _ = Describe("Handler: PreviewImportSource",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() {
			configtest.Setup()
			app = newAPIKeyApp()
		})

		It("403s a non-admin", func() {
			app.addMember("m")
			resp := postSourceJSON(app, "/api/v1/library/imports/sources/preview",
				app.memberKey, arrSourceBody)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("422s with code connection_failed when the key is rejected", func() {
			lib := arrmocks.NewMockLibrary(GinkgoT())
			app.arrClients.EXPECT().
				Client(arr.Radarr, "http://radarr.lan:7878", "k").
				Return(lib, nil).Once()
			lib.EXPECT().TestConnection(mock.Anything).
				Return(arr.ErrUnauthorized).Once()

			resp := postSourceJSON(app, "/api/v1/library/imports/sources/preview",
				"", arrSourceBody)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			// Without the code the SPA falls through to its generic 422 copy
			// and blames the request body for a credential to fix.
			got := decodeBody[Error](resp)
			Expect(got.Code).NotTo(BeNil())
			Expect(*got.Code).To(Equal(codeConnectionFailed))
			Expect(got.Message).To(ContainSubstring("API key"))
		})

		It("422s a target the SSRF guard refuses without connecting", func() {
			resp := postSourceJSON(app, "/api/v1/library/imports/sources/preview",
				"", map[string]any{
					"app": "radarr", "url": "http://169.254.169.254", "api_key": "k",
				})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It(
			"derives one sample path per root folder and translates the rest",
			func() {
				configtest.Setup(map[string]any{
					"quality_profiles": []map[string]any{{
						"name":                 "HD-1080p",
						"min_resolution":       "720p",
						"preferred_resolution": "1080p",
					}},
					"quality_default_profile": "HD-1080p",
				})
				lib := connected(app, arr.Radarr)
				lib.EXPECT().Status(mock.Anything).
					Return(arr.Status{AppName: "Radarr", Version: "5.14.0"}, nil).
					Once()
				lib.EXPECT().RootFolders(mock.Anything).Return([]arr.RootFolder{
					{Path: "/media/movies", Accessible: true},
					{Path: "/media/movies-4k", Accessible: true},
				}, nil).Once()
				lib.EXPECT().
					QualityProfiles(mock.Anything).
					Return([]arr.QualityProfile{
						{
							ID:     4,
							Name:   "HD-1080p",
							Cutoff: 9,
							Items: []arr.QualityItem{
								{
									Quality: &arr.Quality{
										ID:         9,
										Resolution: 1080,
									},
									Allowed: true,
								},
							},
						},
					}, nil).
					Once()
				lib.EXPECT().Indexers(mock.Anything).Return([]arr.Provider{
					{Name: "NZBgeek", Implementation: "Newznab", Protocol: "usenet"},
				}, nil).Once()
				lib.EXPECT().DownloadClients(mock.Anything).Return([]arr.Provider{{
					Name: "qbit", Implementation: "QBittorrent", Protocol: "torrent",
					Enable: true,
					Fields: []arr.Field{
						{Name: "host", Value: "qbit"},
						{Name: "port", Value: float64(8080)},
						{Name: "password", Value: "********"},
					},
				}}, nil).Once()
				lib.EXPECT().Movies(mock.Anything).Return([]arr.Movie{
					{
						TMDBID: 1, Monitored: true, Path: "/media/movies-4k/A",
						MovieFile: &arr.MovieFile{Path: "/media/movies-4k/A/a.mkv"},
					},
					{TMDBID: 2, Path: "/media/movies-4k/B"},
				}, nil).Once()

				resp := postSourceJSON(
					app,
					"/api/v1/library/imports/sources/preview",
					"",
					arrSourceBody,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				got := decodeBody[ArrPreview](resp)

				Expect(got.Version).To(Equal("5.14.0"))
				Expect(
					got.Counts,
				).To(Equal(ArrCounts{Titles: 2, WithFile: 1, Monitored: 1}))
				Expect(got.RootFolders).To(HaveLen(2))
				Expect(got.RootFolders[0].SamplePath).To(BeNil())
				Expect(got.RootFolders[0].TitleCount).To(BeZero())
				Expect(
					*got.RootFolders[1].SamplePath,
				).To(Equal("/media/movies-4k/A/a.mkv"))
				Expect(got.RootFolders[1].TitleCount).To(Equal(2))

				Expect(got.QualityProfiles).To(HaveLen(1))
				Expect(*got.QualityProfiles[0].Existing).To(Equal("HD-1080p"))
				Expect(
					string(got.QualityProfiles[0].Translated.PreferredResolution),
				).
					To(Equal("1080p"))

				Expect(got.Indexers).To(HaveLen(1))
				Expect(
					got.Indexers[0].Kind,
				).To(Equal(ArrIndexerOptionKind("unsupported")))
				Expect(got.DownloadClients).To(HaveLen(1))
				Expect(got.DownloadClients[0].NeedsSecret).To(BeTrue())
			},
		)
	})

var _ = Describe("Handler: CheckImportSourcePaths",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It(
			"reports a found sample, a missing one, and a mapping with no sample",
			func() {
				dir := GinkgoT().TempDir()
				Expect(
					os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("x"), 0o600),
				).
					To(Succeed())

				resp := postSourceJSON(
					app,
					"/api/v1/library/imports/sources/check-paths",
					"",
					map[string]any{"roots": []map[string]any{
						{
							"from":        "/media/movies",
							"to":          dir,
							"sample_path": "/media/movies/a.mkv",
						},
						{
							"from":        "/media/tv",
							"to":          "/nowhere",
							"sample_path": "/media/tv/b.mkv",
						},
						{"from": "/media/empty", "to": "/nowhere"},
					}},
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				got := decodeBody[CheckPathsResponse](resp)

				Expect(got.Roots).To(HaveLen(3))
				Expect(got.Roots[0].Found).To(BeTrue())
				Expect(got.Roots[0].Resolved).To(Equal(filepath.Join(dir, "a.mkv")))
				Expect(got.Roots[1].Found).To(BeFalse())
				Expect(*got.Roots[1].Reason).To(Equal(ArrRootCheckReasonNotFound))
				Expect(got.Roots[2].Found).To(BeTrue())
			},
		)
	})

var _ = Describe("Handler: StartImport from a source",
	Label("unit", "server", "imports"), func() {
		var (
			app *apiKeyApp
			dir string
		)

		BeforeEach(func() {
			dir = GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("x"), 0o600)).
				To(Succeed())
			configtest.SetupFile()
			app = newAPIKeyApp()
		})

		body := func(create bool) map[string]any {
			pm := map[string]any{"source_id": 4, "target": "HD"}
			if create {
				pm["create"] = map[string]any{
					"name": "ignored", "min_resolution": "720p",
					"preferred_resolution": "1080p",
				}
			}
			return map[string]any{
				"mode": "in_place", "source": "radarr",
				"source_url": "http://radarr.lan:7878", "api_key": "secret-key",
				"root_mappings": []map[string]any{{
					"from": "/media/movies", "to": dir,
					"sample_path": "/media/movies/a.mkv",
				}},
				"profile_mappings": []map[string]any{pm},
			}
		}

		It("creates the mapped profile, then starts the scan with the key", func() {
			var seen bulkimport.StartScanParams
			app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
				Run(func(_ context.Context, p bulkimport.StartScanParams) { seen = p }).
				Return(&ent.ImportScan{
					ID: 1, Source: entimportscan.SourceRadarr,
					SourceURL: "http://radarr.lan:7878",
				}, nil).Once()

			resp := postSourceJSON(app, "/api/v1/library/imports", "", body(true))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			p, ok := config.LookupQualityProfile("HD")
			Expect(ok).To(BeTrue())
			Expect(p.Name).To(Equal("HD"))
			Expect(seen.Source).To(Equal(entimportscan.SourceRadarr))
			Expect(seen.APIKey).To(Equal("secret-key"))
			Expect(seen.Mappings.Roots).To(HaveLen(1))
			Expect(seen.Mappings.Profiles[0].Target).To(Equal("HD"))

			// The response is what a browser and every proxy in between sees.
			raw, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(raw)).NotTo(ContainSubstring("secret-key"))
			Expect(string(raw)).To(ContainSubstring(`"source":"radarr"`))
		})

		It(
			"is idempotent over a profile an earlier attempt already created",
			func() {
				app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
					Return(nil, bulkimport.ErrScanRunning).Once()
				resp := postSourceJSON(
					app,
					"/api/v1/library/imports",
					"",
					body(true),
				)
				resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))

				app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
					Return(&ent.ImportScan{ID: 2}, nil).Once()
				resp = postSourceJSON(app, "/api/v1/library/imports", "", body(true))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			},
		)

		It("422s a name collision and starts no scan", func() {
			Expect(config.AddQualityProfile(context.Background(),
				config.QualityProfileEntry{
					Name: "HD", MinResolution: "2160p", PreferredResolution: "2160p",
				})).To(Succeed())

			// No StartScan expectation: the mock fails the spec if called.
			resp := postSourceJSON(app, "/api/v1/library/imports", "", body(true))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(decodeBody[Error](resp).Message).To(ContainSubstring(`"HD"`))
		})

		It("422s a root mapping whose sample does not resolve", func() {
			b := body(false)
			b["root_mappings"] = []map[string]any{{
				"from": "/media/movies", "to": "/nowhere",
				"sample_path": "/media/movies/a.mkv",
			}}
			resp := postSourceJSON(app, "/api/v1/library/imports", "", b)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(
				decodeBody[Error](resp).Message,
			).To(ContainSubstring("/media/movies"))
		})

		It("422s a migration without an API key", func() {
			b := body(false)
			delete(b, "api_key")
			resp := postSourceJSON(app, "/api/v1/library/imports", "", b)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("422s a filesystem scan without a source path", func() {
			resp := postSourceJSON(app, "/api/v1/library/imports", "",
				map[string]any{"mode": "in_place"})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("maps a root outside the library onto a 422", func() {
			app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
				Return(nil, bulkimport.ErrRootOutsideLibrary).Once()
			resp := postSourceJSON(app, "/api/v1/library/imports", "", body(false))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

var _ = Describe("Handler: ApplyImportSourceConfig",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp

		torznab := arr.Provider{
			Name: "Nyaa", Implementation: "Torznab", Protocol: "torrent",
			EnableRSS: true,
			Fields: []arr.Field{
				{Name: "baseUrl", Value: "http://jackett.lan:9117/api"},
				{Name: "apiKey", Value: "abc", Privacy: "apiKey"},
			},
		}
		qbit := arr.Provider{
			Name: "qbit", Implementation: "QBittorrent", Protocol: "torrent",
			Enable: true,
			Fields: []arr.Field{
				{Name: "host", Value: "qbit.lan"},
				{Name: "port", Value: float64(8080)},
				{Name: "username", Value: "admin"},
				{Name: "password", Value: "pw", Privacy: "password"},
			},
		}

		BeforeEach(func() { app = newAPIKeyApp() })

		providers := func(idx, dc []arr.Provider) {
			lib := connected(app, arr.Radarr)
			lib.EXPECT().Indexers(mock.Anything).Return(idx, nil).Maybe()
			lib.EXPECT().DownloadClients(mock.Anything).Return(dc, nil).Maybe()
		}

		req := func(indexers, clients []map[string]any) map[string]any {
			b := map[string]any{
				"app": "radarr", "url": "http://radarr.lan:7878", "api_key": "k",
			}
			if indexers != nil {
				b["indexers"] = indexers
			}
			if clients != nil {
				b["download_clients"] = clients
			}
			return b
		}

		It("writes the selected indexer and client", func() {
			configtest.SetupFile()
			providers([]arr.Provider{torznab}, []arr.Provider{qbit})

			resp := postSourceJSON(
				app,
				"/api/v1/library/imports/sources/apply-config",
				"",
				req(
					[]map[string]any{{"name": "Nyaa"}},
					[]map[string]any{{"name": "qbit"}},
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			got := decodeBody[ApplySourceConfigResponse](resp)
			Expect(got.Indexers).To(ConsistOf("Nyaa"))
			Expect(got.DownloadClients).To(ConsistOf("qbit"))

			ix, ok := config.FindIndexer("Nyaa")
			Expect(ok).To(BeTrue())
			Expect(ix.APIKey).To(Equal("abc"))
			_, ok = config.FindDownloadClient("qbit")
			Expect(ok).To(BeTrue())
		})

		It("422s on a name collision without writing either", func() {
			configtest.SetupFile(map[string]any{
				"indexers": []map[string]any{{
					"name": "Nyaa", "host": "h", "port": 1,
					"api_key": "x", "protocol": "torznab",
				}},
			})
			providers([]arr.Provider{torznab}, []arr.Provider{qbit})

			resp := postSourceJSON(
				app,
				"/api/v1/library/imports/sources/apply-config",
				"",
				req(
					[]map[string]any{{"name": "Nyaa"}},
					[]map[string]any{{"name": "qbit"}},
				),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))

			// Half a config is worse than none: the client must not have landed.
			_, ok := config.FindDownloadClient("qbit")
			Expect(ok).To(BeFalse())
		})

		It(
			"422s an entry needing a secret sent without one, and takes one sent",
			func() {
				configtest.SetupFile()
				masked := torznab
				masked.Fields = []arr.Field{
					{Name: "baseUrl", Value: "http://jackett.lan:9117/api"},
					{Name: "apiKey", Value: "********", Privacy: "apiKey"},
				}
				providers([]arr.Provider{masked}, nil)

				resp := postSourceJSON(
					app,
					"/api/v1/library/imports/sources/apply-config",
					"",
					req([]map[string]any{{"name": "Nyaa"}}, nil),
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				Expect(decodeBody[Error](resp).Message).To(ContainSubstring("Nyaa"))

				providers([]arr.Provider{masked}, nil)
				resp2 := postSourceJSON(
					app,
					"/api/v1/library/imports/sources/apply-config",
					"",
					req([]map[string]any{{"name": "Nyaa", "secret": "real"}}, nil),
				)
				defer resp2.Body.Close()
				Expect(resp2.StatusCode).To(Equal(http.StatusOK))
				ix, ok := config.FindIndexer("Nyaa")
				Expect(ok).To(BeTrue())
				Expect(ix.APIKey).To(Equal("real"))
			},
		)

		It("422s an unsupported selection", func() {
			configtest.SetupFile()
			providers([]arr.Provider{
				{Name: "NZBgeek", Implementation: "Newznab", Protocol: "usenet"},
			}, nil)

			resp := postSourceJSON(
				app,
				"/api/v1/library/imports/sources/apply-config",
				"",
				req([]map[string]any{{"name": "NZBgeek"}}, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(decodeBody[Error](resp).Message).To(ContainSubstring("usenet"))
		})
	})
