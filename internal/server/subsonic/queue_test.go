package subsonic

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/scheduler"
)

type fakeJobs struct {
	running bool
	ran     []string
	err     error
}

func (j *fakeJobs) Get(name string) (scheduler.JobInfo, error) {
	return scheduler.JobInfo{Name: name, Running: j.running}, nil
}

func (j *fakeJobs) RunNow(name string) error {
	j.ran = append(j.ran, name)
	return j.err
}

var _ = g.Describe(
	"bookmarks, queue, radio and system",
	g.Label("integration"),
	func() {
		var f *fixture

		g.BeforeEach(func() {
			f = newFixture(context.Background())
			f.giveFile(f.noFile, "/music/nirvana/02.flac")
		})

		g.It("creates, moves and deletes a bookmark", func() {
			okResp(f.call("createBookmark", url.Values{
				"id": {tr(f.withFile)}, "position": {"61000"}, "comment": {"chorus"},
			}))
			okResp(
				f.call(
					"createBookmark",
					url.Values{"id": {tr(f.withFile)}, "position": {"90000"}},
				),
			)
			bs := list(
				obj(okResp(f.call("getBookmarks", nil)), "bookmarks"),
				"bookmark",
			)
			Expect(bs).To(HaveLen(1))
			Expect(bs[0]["position"]).To(BeEquivalentTo(90000))
			Expect(bs[0]["entry"].(map[string]any)["id"]).To(Equal(tr(f.withFile)))

			Expect(list(obj(okResp(f.callAs(
				f.addUser("other@example.com", "member").Email, "getBookmarks", nil,
			)), "bookmarks"), "bookmark")).To(BeEmpty())

			okResp(f.call("deleteBookmark", url.Values{"id": {tr(f.withFile)}}))
			Expect(
				list(
					obj(okResp(f.call("getBookmarks", nil)), "bookmarks"),
					"bookmark",
				),
			).To(BeEmpty())

			Expect(
				errCode(
					f.call("createBookmark", url.Values{"id": {tr(f.withFile)}}),
				),
			).
				To(BeEquivalentTo(10))
			Expect(
				errCode(
					f.call(
						"createBookmark",
						url.Values{"id": {"tr-999999"}, "position": {"1"}},
					),
				),
			).
				To(BeEquivalentTo(70))
		})

		g.It("saves and restores the play queue by id and by index", func() {
			Expect(okResp(f.call("getPlayQueue", nil))).NotTo(HaveKey("playQueue"))

			okResp(f.call("savePlayQueue", url.Values{
				"id":       {tr(f.withFile), tr(f.noFile), tr(f.withFile)},
				"current":  {tr(f.noFile)},
				"position": {"12000"},
				"c":        {"Tempo"},
			}))
			q := obj(okResp(f.call("getPlayQueue", nil)), "playQueue")
			Expect(q["current"]).To(Equal(tr(f.noFile)))
			Expect(q["position"]).To(BeEquivalentTo(12000))
			Expect(q["changedBy"]).To(Equal("Tempo"))
			Expect(list(q, "entry")).To(HaveLen(3))

			okResp(f.call("savePlayQueueByIndex", url.Values{
				"id":           {tr(f.withFile), tr(f.noFile), tr(f.withFile)},
				"currentIndex": {"2"},
			}))
			qi := obj(okResp(f.call("getPlayQueueByIndex", nil)), "playQueueByIndex")
			Expect(qi["currentIndex"]).To(BeEquivalentTo(2))
			q = obj(okResp(f.call("getPlayQueue", nil)), "playQueue")
			Expect(q["current"]).To(Equal(tr(f.withFile)))

			Expect(errCode(f.call("savePlayQueue", url.Values{
				"id": {tr(f.withFile)}, "current": {tr(f.noFile)},
			}))).To(BeEquivalentTo(0))
			Expect(errCode(f.call("savePlayQueueByIndex", url.Values{
				"id": {tr(f.withFile)}, "currentIndex": {"3"},
			}))).To(BeEquivalentTo(0))

			okResp(f.call("savePlayQueue", nil))
			Expect(okResp(f.call("getPlayQueue", nil))).NotTo(HaveKey("playQueue"))
		})

		g.It("moves the current entry when an earlier one lost its file", func() {
			okResp(f.call("savePlayQueueByIndex", url.Values{
				"id":           {tr(f.noFile), tr(f.withFile)},
				"currentIndex": {"1"},
			}))
			ctx := context.Background()
			_, err := f.client.MediaFile.Delete().Where().Exec(ctx)
			Expect(err).NotTo(HaveOccurred())
			f.giveFile(f.withFile, "/music/nirvana/01.flac")
			qi := obj(okResp(f.call("getPlayQueueByIndex", nil)), "playQueueByIndex")
			Expect(qi["currentIndex"]).To(BeEquivalentTo(0))
			Expect(ids(list(qi, "entry"))).To(Equal([]string{tr(f.withFile)}))
		})

		g.It(
			"lets every user list radio stations and only an admin change them",
			func() {
				admin := f.addUser("admin@example.com", "admin")
				Expect(errCode(f.call("createInternetRadioStation", url.Values{
					"name": {
						"FIP",
					},
					"streamUrl": {"https://icecast.radiofrance.fr/fip-hifi.aac"},
				}))).To(BeEquivalentTo(50))

				okResp(
					f.callAs(admin.Email, "createInternetRadioStation", url.Values{
						"name": {"FIP"},
						"streamUrl": {
							"https://icecast.radiofrance.fr/fip-hifi.aac",
						},
						"homepageUrl": {"https://www.radiofrance.fr/fip"},
					}),
				)
				stations := list(
					obj(
						okResp(f.call("getInternetRadioStations", nil)),
						"internetRadioStations",
					),
					"internetRadioStation",
				)
				Expect(stations).To(HaveLen(1))
				id := stations[0]["id"].(string)
				Expect(
					stations[0]["homePageUrl"],
				).To(Equal("https://www.radiofrance.fr/fip"))

				okResp(
					f.callAs(admin.Email, "updateInternetRadioStation", url.Values{
						"id": {
							id,
						},
						"name":      {"FIP Rock"},
						"streamUrl": {"https://example.org/rock"},
					}),
				)
				stations = list(
					obj(
						okResp(f.call("getInternetRadioStations", nil)),
						"internetRadioStations",
					),
					"internetRadioStation",
				)
				Expect(stations[0]["name"]).To(Equal("FIP Rock"))
				Expect(
					stations[0]["homePageUrl"],
				).To(Equal("https://www.radiofrance.fr/fip"))

				Expect(
					errCode(
						f.callAs(
							admin.Email,
							"createInternetRadioStation",
							url.Values{
								"name": {
									"Bad",
								},
								"streamUrl": {"javascript:alert(1)"},
							},
						),
					),
				).To(BeEquivalentTo(0))
				Expect(
					errCode(
						f.callAs(
							admin.Email,
							"createInternetRadioStation",
							url.Values{
								"name": {"Bad"},
							},
						),
					),
				).To(BeEquivalentTo(10))

				okResp(
					f.callAs(
						admin.Email,
						"deleteInternetRadioStation",
						url.Values{"id": {id}},
					),
				)
				Expect(
					errCode(
						f.callAs(
							admin.Email,
							"deleteInternetRadioStation",
							url.Values{"id": {id}},
						),
					),
				).
					To(BeEquivalentTo(70))
			},
		)

		g.It("describes the caller's own account only", func() {
			u := obj(
				okResp(
					f.call(
						"getUser",
						url.Values{"username": {"listener@example.com"}},
					),
				),
				"user",
			)
			Expect(u["adminRole"]).To(BeFalse())
			Expect(u["shareRole"]).To(BeTrue())
			Expect(u["streamRole"]).To(BeTrue())
			Expect(
				errCode(
					f.call(
						"getUser",
						url.Values{"username": {"someone@example.com"}},
					),
				),
			).
				To(BeEquivalentTo(50))
			Expect(errCode(f.call("getUser", nil))).To(BeEquivalentTo(10))
		})

		g.It("reports and starts a scan through the drift check", func() {
			jobs := &fakeJobs{running: true}
			f.handler = New(Deps{
				Ent: f.client, Tracker: appaccess.NewTracker(f.client), Jobs: jobs,
			}).Routes()
			admin := f.addUser("admin@example.com", "admin")

			st := obj(okResp(f.call("getScanStatus", nil)), "scanStatus")
			Expect(st["scanning"]).To(BeTrue())
			Expect(st["count"]).To(BeEquivalentTo(2))

			Expect(errCode(f.call("startScan", nil))).To(BeEquivalentTo(50))
			jobs.err = scheduler.ErrJobBusy
			st = obj(okResp(f.callAs(admin.Email, "startScan", nil)), "scanStatus")
			Expect(st["scanning"]).To(BeTrue())
			Expect(jobs.ran).To(Equal([]string{"drift-check"}))
		})

		g.It("never reports a scan without a scheduler", func() {
			st := obj(okResp(f.call("getScanStatus", nil)), "scanStatus")
			Expect(st["scanning"]).To(BeFalse())
		})
	},
)

var _ = g.Describe("play queue client name", g.Label("integration"), func() {
	g.It("never splits a character when it trims the client name", func() {
		f := newFixture(context.Background())
		name := strings.Repeat("a", 63) + "é"
		okResp(f.call("savePlayQueue", url.Values{
			"id": {tr(f.withFile)}, "c": {name},
		}))
		q := obj(okResp(f.call("getPlayQueue", nil)), "playQueue")
		changedBy := q["changedBy"].(string)
		Expect(utf8.ValidString(changedBy)).To(BeTrue(), "%q", changedBy)
		Expect(len(changedBy)).To(BeNumerically("<=", 64))
	})
})
