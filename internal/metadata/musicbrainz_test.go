package metadata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mbRoundTripper func(*http.Request) (*http.Response, error)

func (rt mbRoundTripper) RoundTrip(
	r *http.Request,
) (*http.Response, error) {
	return rt(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

var _ = Describe("MusicBrainz provider", Label("unit", "metadata"), func() {
	var (
		mb  *MusicBrainz
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		mb = NewMusicBrainz()
		mb.limiter = newMBLimiter(0, 0)
	})

	Describe("SearchArtists", func() {
		It("maps search hits and sends the mandatory User-Agent", func() {
			var gotUA, gotPath string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotUA = r.Header.Get("User-Agent")
					gotPath = r.URL.Path
					return jsonResponse(
						200,
						`{"artists":[{"id":"5b11f4ce-a62d-471e-81fc-a69a8278c7da","name":"Nirvana","sort-name":"Nirvana","disambiguation":"90s US grunge band","score":100}]}`,
					), nil
				},
			)
			res, err := mb.SearchArtists(ctx, "nirvana")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].MBID).To(Equal("5b11f4ce-a62d-471e-81fc-a69a8278c7da"))
			Expect(res[0].Name).To(Equal("Nirvana"))
			Expect(gotUA).To(ContainSubstring("streamline/"))
			Expect(gotPath).To(Equal("/ws/2/artist"))
		})

		It("fails on a non-200", func() {
			mb.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(500, `{}`), nil
				},
			)
			_, err := mb.SearchArtists(ctx, "nirvana")
			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(ErrRateLimited))
		})

		It("maps a 503 to a rate-limit error carrying Retry-After", func() {
			mb.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					resp := jsonResponse(503, `{}`)
					resp.Header.Set("Retry-After", "7")
					return resp, nil
				},
			)
			_, err := mb.SearchArtists(ctx, "nirvana")
			Expect(err).To(MatchError(ErrRateLimited))
			var limited *RateLimitedError
			Expect(errors.As(err, &limited)).To(BeTrue())
			Expect(limited.RetryAfter).To(Equal(7 * time.Second))
		})

		It("waits a minute when a 503 names no Retry-After", func() {
			mb.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(503, `{}`), nil
				},
			)
			_, err := mb.SearchArtists(ctx, "nirvana")
			var limited *RateLimitedError
			Expect(errors.As(err, &limited)).To(BeTrue())
			Expect(limited.RetryAfter).To(Equal(time.Minute))
		})

		It("reads type, area, start year and the most voted real genre", func() {
			genreCalls := 0
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/ws/2/genre/all" {
						genreCalls++
						Expect(r.URL.Query().Get("fmt")).To(Equal("txt"))
						return &http.Response{
							StatusCode: 200,
							Body: io.NopCloser(
								strings.NewReader("rock\nalternative rock\n"),
							),
						}, nil
					}
					return jsonResponse(
						200,
						`{"artists":[{"id":"a1","name":"Nirvana","sort-name":"Nirvana","score":100,"type":"Group","begin-area":{"name":"Aberdeen"},"area":{"name":"United States"},"life-span":{"begin":"1987-01"},"tags":[{"name":"seen live","count":9},{"name":"rock","count":8},{"name":"alternative rock","count":5}]}]}`,
					), nil
				},
			)
			res, err := mb.SearchArtists(ctx, "nirvana")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].Type).To(Equal("group"))
			Expect(res[0].Area).To(Equal("Aberdeen, United States"))
			Expect(res[0].Since).To(Equal(uint16(1987)))
			Expect(res[0].Genre).To(Equal("Rock"))

			_, err = mb.SearchArtists(ctx, "nirvana")
			Expect(err).NotTo(HaveOccurred())
			Expect(genreCalls).To(Equal(1), "the genre list is cached")
		})

		It("still answers when the genre list cannot be fetched", func() {
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/ws/2/genre/all" {
						return jsonResponse(500, `{}`), nil
					}
					return jsonResponse(
						200,
						`{"artists":[{"id":"a1","name":"X","tags":[{"name":"rock","count":1}]}]}`,
					), nil
				},
			)
			res, err := mb.SearchArtists(ctx, "x")
			Expect(err).NotTo(HaveOccurred())
			Expect(res[0].Genre).To(BeEmpty())
		})

		It("leaves the type empty for a character or an unstated type", func() {
			mb.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(
						200,
						`{"artists":[{"id":"a1","name":"Mario","type":"Character"},{"id":"a2","name":"Bjork","type":"Person"},{"id":"a3","name":"LSO","type":"Orchestra"},{"id":"a4","name":"?"}]}`,
					), nil
				},
			)
			res, err := mb.SearchArtists(ctx, "x")
			Expect(err).NotTo(HaveOccurred())
			kinds := []string{res[0].Type, res[1].Type, res[2].Type, res[3].Type}
			Expect(kinds).To(Equal([]string{"", "person", "group", ""}))
		})
	})

	Describe("GetArtist", func() {
		const artistJSON = `{"id":"mbid-1","name":"Nirvana","sort-name":"Nirvana","type":"Group",
			"begin-area":{"name":"Aberdeen"},"area":{"name":"United States"},
			"life-span":{"begin":"1987-01-01"},
			"genres":[{"name":"grunge","count":3},{"name":"rock","count":9}],
			"relations":[
				{"type":"member of band","direction":"backward","begin":"1987","end":"1994","ended":true,"attributes":["original","guitar","lead vocals"],"artist":{"id":"m-kurt","name":"Kurt Cobain"}},
				{"type":"member of band","direction":"backward","begin":"1990","ended":false,"attributes":["additional","drums"],"artist":{"id":"m-dave","name":"Dave Grohl"}},
				{"type":"member of band","direction":"forward","artist":{"id":"other","name":"Other Band"}},
				{"type":"wikidata","url":{"resource":"https://www.wikidata.org/wiki/Q11649"}},
				{"type":"streaming","url":{"resource":"https://www.deezer.com/en/artist/415"}}
			]}`

		It(
			"merges the artist lookup with the paginated release-group browse",
			func() {
				var calls []string
				var gotInc string
				mb.client.Transport = mbRoundTripper(
					func(r *http.Request) (*http.Response, error) {
						calls = append(calls, r.URL.Path)
						if r.URL.Path == "/ws/2/artist/mbid-1" {
							gotInc = r.URL.Query().Get("inc")
							return jsonResponse(200, artistJSON), nil
						}
						return jsonResponse(
							200,
							`{"release-group-count":1,"release-groups":[{"id":"rg-1","title":"Nevermind","primary-type":"Album","first-release-date":"1991-09-24"}]}`,
						), nil
					},
				)
				a, err := mb.GetArtist(ctx, "mbid-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(gotInc).To(Equal("genres+artist-rels+url-rels"))
				Expect(a.ReleaseGroups).To(HaveLen(1))
				Expect(a.ReleaseGroups[0].Type).To(Equal(AlbumTypeAlbum))
				Expect(a.ReleaseGroups[0].ReleaseDate.Year()).To(Equal(1991))
				Expect(calls).To(ContainElement("/ws/2/release-group"))

				Expect(a.Type).To(Equal("group"))
				Expect(a.Origin).To(Equal("Aberdeen, United States"))
				Expect(a.Since).To(Equal(uint16(1987)))
				Expect(a.Genre).To(Equal("Rock"))
				Expect(a.Genres).To(Equal([]string{"Rock", "Grunge"}))
				Expect(a.DeezerID).To(Equal(uint32(415)))
				Expect(a.WikidataID).To(Equal("Q11649"))
			},
		)

		It(
			"reads the members of a band, dropping the original/additional flags",
			func() {
				mb.client.Transport = mbRoundTripper(
					func(r *http.Request) (*http.Response, error) {
						if r.URL.Path == "/ws/2/artist/mbid-1" {
							return jsonResponse(200, artistJSON), nil
						}
						return jsonResponse(
							200,
							`{"release-group-count":0,"release-groups":[]}`,
						), nil
					},
				)
				a, err := mb.GetArtist(ctx, "mbid-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(a.Members).To(Equal([]ArtistMemberInfo{
					{
						Name: "Kurt Cobain", MBID: "m-kurt",
						Instruments: []string{"guitar", "lead vocals"},
						FromYear:    1987, ToYear: 1994, Ended: true,
					},
					{
						Name: "Dave Grohl", MBID: "m-dave",
						Instruments: []string{"drums"}, FromYear: 1990,
					},
				}))
			},
		)

		It("pages through the release groups", func() {
			var offsets []string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/ws/2/artist/mbid-1" {
						return jsonResponse(200, `{"id":"mbid-1","name":"X"}`), nil
					}
					offsets = append(offsets, r.URL.Query().Get("offset"))
					if r.URL.Query().Get("offset") == "0" {
						return jsonResponse(
							200,
							`{"release-group-count":3,"release-groups":[{"id":"a","title":"A"},{"id":"b","title":"B"}]}`,
						), nil
					}
					return jsonResponse(
						200,
						`{"release-group-count":3,"release-groups":[{"id":"c","title":"C"}]}`,
					), nil
				},
			)
			a, err := mb.GetArtist(ctx, "mbid-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(a.ReleaseGroups).To(HaveLen(3))
			Expect(offsets).To(Equal([]string{"0", "2"}))
		})

		It("reports a missing artist as ErrNotFound", func() {
			mb.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(404, `{}`), nil
				},
			)
			_, err := mb.GetArtist(ctx, "mbid-1")
			Expect(err).To(MatchError(ErrNotFound))
		})
	})

	Describe("GetReleaseGroup", func() {
		const heavyRelease = `{"id":"rel-1","status":"Official","date":"1991-09-24","barcode":"0720642442524","country":"US",
			"label-info":[{"catalog-number":"DGCD-24425","label":{"name":"DGC"}}],
			"relations":[
				{"type":"producer","target-type":"artist","artist":{"id":"a-butch","name":"Butch Vig"}},
				{"type":"design/illustration","target-type":"artist","artist":{"id":"a-rob","name":"Robert Fisher"}},
				{"type":"producer","target-type":"artist","artist":{"id":"a-butch","name":"Butch Vig"}}
			],
			"media":[
				{"position":1,"title":"","format":"CD","tracks":[
					{"position":1,"title":"Smells Like Teen Spirit","length":301000,
					 "artist-credit":[{"name":"Nirvana","joinphrase":" feat. ","artist":{"id":"a-n","name":"Nirvana"}},{"name":"A Guest","joinphrase":"","artist":{"id":"a-g","name":"A Guest"}}],
					 "recording":{"id":"rec-1","relations":[
						{"type":"instrument","target-type":"artist","attributes":["guitar"],"artist":{"id":"a-kurt","name":"Kurt Cobain"}},
						{"type":"vocal","target-type":"artist","attributes":["lead vocals"],"artist":{"id":"a-kurt","name":"Kurt Cobain"}},
						{"type":"recording","target-type":"place","place":{"name":"Sound City"}},
						{"type":"performance","target-type":"work","work":{"relations":[
							{"type":"writer","target-type":"artist","artist":{"id":"a-kurt","name":"Kurt Cobain"}},
							{"type":"lyricist","target-type":"artist","artist":{"id":"a-kurt","name":"Kurt Cobain"}},
							{"type":"composer","target-type":"artist","artist":{"id":"a-krist","name":"Krist Novoselic"}}]}},
						{"type":"mix","target-type":"artist","artist":{"id":"a-andy","name":"Andy Wallace"}}]}},
					{"position":2,"title":"In Bloom (Bonus Track)","length":254000,"recording":{"id":"rec-2"}}]},
				{"position":2,"title":"Bonus disc","format":"CD","tracks":[
					{"position":1,"title":"Curse","recording":{"id":"rec-3"}}]}]}`

		const releaseGroupJSON = `{"id":"rg-1","title":"Nevermind","primary-type":"Album","first-release-date":"1991-09-24","releases":[
			{"id":"rel-2","status":"Official","date":"1992-01-01","media":[{"format":"12\" Vinyl"},{"format":"Digital Media"}]},
			{"id":"rel-1","status":"Official","date":"1991-09-24","media":[{"format":"CD"}]},
			{"id":"rel-0","status":"Bootleg","date":"1991-01-01","media":[{"format":"Cassette"}]}]}`

		serve := func(release func(inc string) *http.Response) *[]string {
			var incs []string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					switch r.URL.Path {
					case "/ws/2/release-group/rg-1":
						Expect(r.URL.Query().Get("inc")).To(Equal("releases+media"))
						return jsonResponse(200, releaseGroupJSON), nil
					case "/ws/2/release/rel-1":
						inc := r.URL.Query().Get("inc")
						incs = append(incs, inc)
						return release(inc), nil
					}
					return jsonResponse(404, `{}`), nil
				},
			)
			return &incs
		}

		It("picks the earliest official release and flattens its media", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.ReleaseMBID).To(Equal("rel-1"))
			Expect(rg.Tracks).To(HaveLen(3))
			Expect(rg.Tracks[0].Duration).To(Equal(uint32(301)))
			Expect(rg.Tracks[0].Disc).To(Equal(uint8(1)))
			Expect(rg.Tracks[2].Disc).To(Equal(uint8(2)))
		})

		It("asks the heavy relationship expansion first", func() {
			incs := serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			_, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(*incs).To(Equal([]string{
				"recordings+artist-credits+labels+artist-rels+recording-level-rels+work-rels+work-level-rels+place-rels",
			}))
		})

		It("reads the album facts: barcode, label, country and media", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Barcode).To(Equal("0720642442524"))
			Expect(rg.Label).To(Equal("DGC"))
			Expect(rg.CatalogNumber).To(Equal("DGCD-24425"))
			Expect(rg.Country).To(Equal("US"))
			Expect(
				rg.Media,
			).To(Equal([]string{"cd", "vinyl", "digital", "cassette"}))
			Expect(rg.CreditsComplete).To(BeTrue())
		})

		It("drops a country that is not a region", func() {
			serve(func(string) *http.Response {
				return jsonResponse(200, `{"id":"rel-1","country":"XW","media":[]}`)
			})
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Country).To(BeEmpty())
		})

		It("reads featured artists only after a feat. join phrase", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(
				rg.Tracks[0].Featuring,
			).To(Equal([]PersonInfo{{Name: "A Guest", MBID: "a-g"}}))
			Expect(rg.Tracks[1].Featuring).To(BeEmpty())
		})

		It("keeps the artists of a collaboration out of featuring", func() {
			serve(func(string) *http.Response {
				return jsonResponse(
					200,
					`{"id":"rel-1","media":[{"position":1,"tracks":[{"position":1,"title":"T","recording":{"id":"r"},"artist-credit":[{"name":"A","joinphrase":" & ","artist":{"id":"a","name":"A"}},{"name":"B","joinphrase":"","artist":{"id":"b","name":"B"}}]}]}]}`,
				)
			})
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Tracks[0].Featuring).To(BeEmpty())
		})

		It("marks bonus tracks only when MusicBrainz says so", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(
				[]bool{rg.Tracks[0].Bonus, rg.Tracks[1].Bonus, rg.Tracks[2].Bonus},
			).
				To(Equal([]bool{false, true, true}))
		})

		It("reads writers, deduped per track", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Tracks[0].Writers).To(Equal([]PersonInfo{
				{Name: "Kurt Cobain", MBID: "a-kurt"},
				{Name: "Krist Novoselic", MBID: "a-krist"},
			}))
		})

		It("reads production credits, deduped on person and role", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Credits).To(ConsistOf(
				CreditInfo{
					Name: "Butch Vig", MBID: "a-butch",
					Role: "producer",
				},
				CreditInfo{
					Name: "Robert Fisher", MBID: "a-rob",
					Role: "artwork",
				},
				CreditInfo{
					Name: "Andy Wallace", MBID: "a-andy",
					Role: "mix",
				},
			))
		})

		It("folds performers over the recordings and counts them", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Performers).To(Equal([]PerformerInfo{{
				Name: "Kurt Cobain", MBID: "a-kurt",
				Instruments: []string{"guitar", "lead vocals"},
				Recordings:  1,
			}}))
			Expect(rg.PerformerRecordings).To(Equal(1))
		})

		It("takes the studio from recorded-at place relationships", func() {
			serve(
				func(string) *http.Response { return jsonResponse(200, heavyRelease) },
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.Studio).To(Equal("Sound City"))
		})

		It("retries once with the light call when the heavy one fails", func() {
			incs := serve(func(inc string) *http.Response {
				if inc == "recordings+artist-credits+labels" {
					return jsonResponse(
						200,
						`{"id":"rel-1","label-info":[{"label":{"name":"DGC"}}],"media":[{"position":1,"tracks":[{"position":1,"title":"T","length":1000,"recording":{"id":"r"}}]}]}`,
					)
				}
				return jsonResponse(500, `{}`)
			})
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(*incs).To(HaveLen(2))
			Expect((*incs)[1]).To(Equal("recordings+artist-credits+labels"))
			Expect(rg.CreditsComplete).To(BeFalse())
			Expect(rg.Label).To(Equal("DGC"))
			Expect(rg.Tracks).To(HaveLen(1))
			Expect(rg.Credits).To(BeEmpty())
			Expect(rg.Performers).To(BeEmpty())
		})

		It("does not retry a rate limit", func() {
			incs := serve(
				func(string) *http.Response { return jsonResponse(503, `{}`) },
			)
			_, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).To(MatchError(ErrRateLimited))
			Expect(*incs).To(HaveLen(1))
		})

		It("does not retry a missing release", func() {
			incs := serve(
				func(string) *http.Response { return jsonResponse(404, `{}`) },
			)
			_, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).To(MatchError(ErrNotFound))
			Expect(*incs).To(HaveLen(1))
		})

		It(
			"falls back to the canonical release's media when no release lists any",
			func() {
				mb.client.Transport = mbRoundTripper(
					func(r *http.Request) (*http.Response, error) {
						switch r.URL.Path {
						case "/ws/2/release-group/rg-1":
							return jsonResponse(
								200,
								`{"id":"rg-1","title":"X","releases":[{"id":"rel-1","status":"Official","date":"1991"}]}`,
							), nil
						case "/ws/2/release/rel-1":
							return jsonResponse(
								200,
								`{"id":"rel-1","media":[{"position":1,"format":"CD","tracks":[]}]}`,
							), nil
						}
						return jsonResponse(404, `{}`), nil
					},
				)
				rg, err := mb.GetReleaseGroup(ctx, "rg-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(rg.Media).To(Equal([]string{"cd"}))
			},
		)

		It(
			"answers a release group with no releases without a release call",
			func() {
				mb.client.Transport = mbRoundTripper(
					func(r *http.Request) (*http.Response, error) {
						if r.URL.Path == "/ws/2/release-group/rg-1" {
							return jsonResponse(
								200,
								`{"id":"rg-1","title":"X","releases":[]}`,
							), nil
						}
						Fail("unexpected request " + r.URL.Path)
						return nil, nil
					},
				)
				rg, err := mb.GetReleaseGroup(ctx, "rg-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(rg.Tracks).To(BeEmpty())
				Expect(rg.ReleaseMBID).To(BeEmpty())
			},
		)

		It("ranks an undated official release after a dated one", func() {
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					switch r.URL.Path {
					case "/ws/2/release-group/rg-1":
						return jsonResponse(
							200,
							`{"id":"rg-1","title":"X","primary-type":"Album","releases":[{"id":"undated","status":"Official","date":""},{"id":"dated","status":"Official","date":"1999-05-01"}]}`,
						), nil
					case "/ws/2/release/dated":
						return jsonResponse(200, `{"id":"dated","media":[]}`), nil
					}
					return jsonResponse(404, `{}`), nil
				},
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.ReleaseMBID).To(Equal("dated"))
		})
	})

	Describe("SearchReleaseGroups", func() {
		It("queries by artist and title", func() {
			var gotQuery, gotPath string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotQuery = r.URL.Query().Get("query")
					gotPath = r.URL.Path
					return jsonResponse(
						200,
						`{"release-groups":[{"id":"rg-1","title":"Nevermind","primary-type":"Album","first-release-date":"1991-09-24","artist-credit":[{"artist":{"id":"mbid-1","name":"Nirvana"}}],"score":100}]}`,
					), nil
				},
			)
			res, err := mb.SearchReleaseGroups(ctx, "Nirvana", "Nevermind")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].MBID).To(Equal("rg-1"))
			Expect(res[0].ArtistMBID).To(Equal("mbid-1"))
			Expect(res[0].ArtistName).To(Equal("Nirvana"))
			Expect(res[0].Score).To(BeEquivalentTo(100))
			Expect(gotPath).To(Equal("/ws/2/release-group"))
			Expect(gotQuery).To(ContainSubstring(`releasegroup:"Nevermind"`))
			Expect(gotQuery).To(ContainSubstring(`artist:"Nirvana"`))
		})

		It("searches by title alone when the artist is empty", func() {
			var gotQuery string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotQuery = r.URL.Query().Get("query")
					return jsonResponse(200, `{"release-groups":[]}`), nil
				},
			)
			_, err := mb.SearchReleaseGroups(ctx, "", "Nevermind")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotQuery).To(Equal(`releasegroup:"Nevermind"`))
		})

		It("escapes embedded quotes", func() {
			var gotQuery string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotQuery = r.URL.Query().Get("query")
					return jsonResponse(200, `{"release-groups":[]}`), nil
				},
			)
			_, err := mb.SearchReleaseGroups(ctx, "", `The "Best" Of`)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotQuery).To(ContainSubstring(`releasegroup:"The \"Best\" Of"`))
		})
	})
})
