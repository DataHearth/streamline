package metadata

import (
	"context"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/time/rate"
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
		mb.limiter = rate.NewLimiter(rate.Inf, 1)
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
					return jsonResponse(503, `{}`), nil
				},
			)
			_, err := mb.SearchArtists(ctx, "nirvana")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetArtist", func() {
		It("merges artist lookup with paginated release-group browse", func() {
			var calls []string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					calls = append(calls, r.URL.Path)
					if r.URL.Path == "/ws/2/artist/mbid-1" {
						return jsonResponse(
							200,
							`{"id":"mbid-1","name":"Nirvana","sort-name":"Nirvana"}`,
						), nil
					}
					return jsonResponse(
						200,
						`{"release-group-count":1,"release-groups":[{"id":"rg-1","title":"Nevermind","primary-type":"Album","first-release-date":"1991-09-24"}]}`,
					), nil
				},
			)
			a, err := mb.GetArtist(ctx, "mbid-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(a.ReleaseGroups).To(HaveLen(1))
			Expect(a.ReleaseGroups[0].Type).To(Equal(AlbumTypeAlbum))
			Expect(a.ReleaseGroups[0].ReleaseDate.Year()).To(Equal(1991))
			Expect(calls).To(ContainElement("/ws/2/release-group"))
		})
	})

	Describe("GetReleaseGroup", func() {
		It("picks the earliest official release and flattens its media", func() {
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					switch r.URL.Path {
					case "/ws/2/release-group/rg-1":
						return jsonResponse(
							200,
							`{"id":"rg-1","title":"Nevermind","primary-type":"Album","first-release-date":"1991-09-24","releases":[{"id":"rel-2","status":"Official","date":"1992-01-01"},{"id":"rel-1","status":"Official","date":"1991-09-24"},{"id":"rel-0","status":"Bootleg","date":"1991-01-01"}]}`,
						), nil
					case "/ws/2/release/rel-1":
						return jsonResponse(
							200,
							`{"id":"rel-1","media":[{"position":1,"tracks":[{"position":1,"title":"Smells Like Teen Spirit","length":301000,"recording":{"id":"rec-1"}}]}]}`,
						), nil
					}
					return jsonResponse(404, `{}`), nil
				},
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.ReleaseMBID).To(Equal("rel-1"))
			Expect(rg.Tracks).To(HaveLen(1))
			Expect(rg.Tracks[0].Duration).To(Equal(uint32(301)))
			Expect(rg.Tracks[0].Disc).To(Equal(uint8(1)))
		})

		It("carries the credited artist and asks for artist-credits", func() {
			var gotInc string
			mb.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/ws/2/release-group/rg-1" {
						gotInc = r.URL.Query().Get("inc")
						return jsonResponse(
							200,
							`{"id":"rg-1","title":"Nevermind","primary-type":"Album","artist-credit":[{"artist":{"id":"artist-1","name":"Nirvana"}}],"releases":[]}`,
						), nil
					}
					return jsonResponse(404, `{}`), nil
				},
			)
			rg, err := mb.GetReleaseGroup(ctx, "rg-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(rg.ArtistMBID).To(Equal("artist-1"))
			Expect(gotInc).To(Equal("releases+artist-credits"))
		})

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
