package restapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"Handler: missing files",
	Label("unit", "server", "library"),
	func() {
		const (
			listURL   = "/api/v1/library/missing-files"
			forgetURL = "/api/v1/library/missing-files/forget"
		)

		var (
			app       *apiKeyApp
			confirmed time.Time
			row       *ent.MediaFile
		)

		BeforeEach(func() {
			app = newAPIKeyApp()
			confirmed = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			since := confirmed.Add(-time.Hour)
			row = &ent.MediaFile{
				ID: 7, Path: "/m/dune.mkv", Size: 42,
				MissingSince: &since, MissingConfirmedAt: &confirmed,
				Edges: ent.MediaFileEdges{Movie: &ent.Movie{ID: 3, Title: "Dune"}},
			}
		})

		setWait := func(value string) {
			GinkgoHelper()
			configtest.Setup(map[string]any{"library": map[string]any{
				"missing_file_redownload_after": value,
			}})
		}
		status := func(method, url, key, body string) int {
			GinkgoHelper()
			resp := app.do(app.req(method, url, key, strings.NewReader(body)))
			defer resp.Body.Close()
			return resp.StatusCode
		}

		It("refuses a non-admin on both routes", func() {
			app.addMember("")

			Expect(status(http.MethodGet, listURL, app.memberKey, "")).
				To(Equal(http.StatusForbidden))
			Expect(status(http.MethodPost, forgetURL, app.memberKey, `{"ids":[7]}`)).
				To(Equal(http.StatusForbidden))
		})

		DescribeTable("refuses an id count outside 1 to 100",
			func(count int) {
				ids := strings.TrimSuffix(strings.Repeat("7,", count), ",")

				Expect(
					status(
						http.MethodPost,
						forgetURL,
						app.adminKey,
						`{"ids":[`+ids+`]}`,
					),
				).
					To(Equal(http.StatusBadRequest))
			},
			Entry("none", 0),
			Entry("101", 101),
		)

		It("refuses a limit past the ceiling", func() {
			Expect(status(http.MethodGet, listURL+"?limit=101", app.adminKey, "")).
				To(Equal(http.StatusBadRequest))
		})

		list := func(url string) MissingFileList {
			GinkgoHelper()
			resp := app.do(app.req(http.MethodGet, url, app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body MissingFileList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			return body
		}

		It("pages the list and carries when the file is downloaded again", func() {
			setWait("72h")
			app.store.EXPECT().ListMissingFiles(mock.Anything, 10, 20).
				Return([]*ent.MediaFile{row}, 21, nil).Once()

			body := list(listURL + "?limit=10&page=3")

			Expect(body.Total).To(Equal(21))
			Expect(body.Items).To(HaveLen(1))
			got := body.Items[0]
			Expect(got.Id).To(Equal(uint32(7)))
			Expect(string(got.Root)).To(Equal("movies"))
			Expect(got.Title).To(Equal("Dune"))
			Expect(string(got.OwnerKind)).To(Equal("movie"))
			Expect(got.OwnerId).To(Equal(uint32(3)))
			Expect(got.RedownloadAt).NotTo(BeNil())
			Expect(
				got.RedownloadAt.Equal(confirmed.Add(72 * time.Hour)),
			).To(BeTrue())
		})

		It("omits the date under off", func() {
			setWait("off")
			app.store.EXPECT().ListMissingFiles(mock.Anything, 50, 0).
				Return([]*ent.MediaFile{row}, 1, nil).Once()

			Expect(list(listURL).Items[0].RedownloadAt).To(BeNil())
		})

		DescribeTable("names each owner",
			func(edges ent.MediaFileEdges, title, kind string, owner uint32) {
				setWait("168h")
				row.Edges = edges
				app.store.EXPECT().ListMissingFiles(mock.Anything, 50, 0).
					Return([]*ent.MediaFile{row}, 1, nil).Once()

				got := list(listURL).Items[0]

				Expect(got.Title).To(Equal(title))
				Expect(string(got.OwnerKind)).To(Equal(kind))
				Expect(got.OwnerId).To(Equal(owner))
			},
			Entry("an episode", ent.MediaFileEdges{Episode: &ent.Episode{
				Number: 2, Edges: ent.EpisodeEdges{Season: &ent.Season{
					Number: 1, Edges: ent.SeasonEdges{
						TvShow: &ent.TVShow{ID: 9, Title: "The Bear"},
					},
				}},
			}}, "The Bear S01E02", "series", uint32(9)),
			Entry("a track", ent.MediaFileEdges{Track: &ent.Track{
				Title:    "Lithium",
				Position: 3,
				Edges: ent.TrackEdges{Album: &ent.Album{
					Title: "Nevermind", Edges: ent.AlbumEdges{
						Artist: &ent.Artist{ID: 8, Name: "Nirvana"},
					},
				}},
			}}, "Nirvana - Nevermind - 03 Lithium", "artist", uint32(8)),
			Entry("a book", ent.MediaFileEdges{Book: &ent.Book{
				ID: 5, Title: "Elantris",
			}}, "Elantris", "book", uint32(5)),
		)

		It("forgets through the store and answers the count", func() {
			app.store.EXPECT().FindMediaFileWithOwners(mock.Anything, uint32(7)).
				Return(row, nil).Once()
			app.store.EXPECT().ForgetMediaFile(mock.Anything, row).Return(nil).Once()

			resp := app.do(app.req(
				http.MethodPost, forgetURL, app.adminKey,
				strings.NewReader(`{"ids":[7]}`),
			))
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ForgetMissingFilesResult
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Forgotten).To(Equal(1))
		})
	},
)
