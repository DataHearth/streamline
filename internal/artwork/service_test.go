package artwork_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/artwork"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/posters"
)

const (
	artistMBID = "11111111-1111-1111-1111-111111111111"
	albumMBID  = "22222222-2222-2222-2222-222222222222"
	imageBytes = "\xff\xd8\xff\xe0jpeg"
)

func fileTime(hoursFromNow float64) time.Time {
	return time.Now().Add(time.Duration(hoursFromNow * float64(time.Hour)))
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, contentType, body string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type fakeDeezer struct {
	byID   map[uint32]*metadata.DeezerArtist
	search map[string][]metadata.DeezerArtist
	calls  atomic.Int32
}

func (d *fakeDeezer) ArtistByID(
	_ context.Context,
	id uint32,
) (*metadata.DeezerArtist, error) {
	d.calls.Add(1)
	return d.byID[id], nil
}

func (d *fakeDeezer) SearchArtists(
	_ context.Context,
	name string,
) ([]metadata.DeezerArtist, error) {
	d.calls.Add(1)
	return d.search[name], nil
}

type fakeLibrary struct {
	ids map[string]uint32
}

func (l fakeLibrary) Find(
	_ context.Context,
	kind artwork.Kind,
	key string,
) (uint32, bool, error) {
	id, ok := l.ids[string(kind)+"/"+key]
	return id, ok, nil
}

var _ = Describe("Service", Label("unit", "artwork"), func() {
	var (
		svc     *artwork.Service
		sources *artwork.Sources
		deezer  *fakeDeezer
		lib     fakeLibrary
		pm      posters.Manager
		dir     string
		fetched []string
		reply   roundTrip
	)

	get := func(kind, key string) *httptest.ResponseRecorder {
		GinkgoHelper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodGet, "/posters/lookup/"+kind+"/"+key+"/poster.jpg", nil,
		)
		svc.Serve(rec, req, kind, key)
		return rec
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		var err error
		pm, err = posters.New(dir)
		Expect(err).NotTo(HaveOccurred())
		sources = artwork.NewSources()
		deezer = &fakeDeezer{
			byID:   map[uint32]*metadata.DeezerArtist{},
			search: map[string][]metadata.DeezerArtist{},
		}
		lib = fakeLibrary{ids: map[string]uint32{}}
		fetched = nil
		reply = func(r *http.Request) (*http.Response, error) {
			return respond(200, "image/jpeg", imageBytes), nil
		}
		build := func() {
			svc, err = artwork.New(artwork.Deps{
				DataDir: dir,
				Posters: pm,
				Library: lib,
				Deezer:  deezer,
				Sources: sources,
				Client: &http.Client{Transport: roundTrip(
					func(r *http.Request) (*http.Response, error) {
						fetched = append(fetched, r.URL.String())
						return reply(r)
					},
				)},
			})
			Expect(err).NotTo(HaveOccurred())
		}
		build()
		DeferCleanup(func() { fetched = nil })
	})

	It("404s an unknown kind and a malformed key without fetching", func() {
		Expect(get("lookup-books", "1").Code).To(Equal(http.StatusNotFound))
		Expect(get("movies", "1").Code).To(Equal(http.StatusNotFound))
		Expect(
			get("artists", "../../etc/passwd").Code,
		).To(Equal(http.StatusNotFound))
		Expect(get("artists", "not-an-mbid").Code).To(Equal(http.StatusNotFound))
		Expect(get("books", "abc").Code).To(Equal(http.StatusNotFound))
		Expect(get("books", "12345678901").Code).To(Equal(http.StatusNotFound))
		Expect(fetched).To(BeEmpty())
	})

	Describe("albums", func() {
		It("fetches the Cover Art Archive front and caches it", func() {
			rec := get("albums", albumMBID)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(Equal(imageBytes))
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/jpeg"))
			Expect(fetched).To(Equal([]string{
				"https://coverartarchive.org/release-group/" + albumMBID + "/front-500",
			}))
			Expect(
				filepath.Join(
					dir,
					"posters",
					"lookup",
					"albums",
					albumMBID,
					"poster.jpg",
				),
			).
				To(BeARegularFile())

			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))
			Expect(fetched).To(HaveLen(1))
		})

		It("404s when the archive has no cover", func() {
			reply = func(*http.Request) (*http.Response, error) {
				return respond(404, "text/html", "no"), nil
			}
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusNotFound))
		})

		It("remembers a failure so a page of hits does not retry", func() {
			reply = func(*http.Request) (*http.Response, error) {
				return respond(404, "text/html", "no"), nil
			}
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusNotFound))
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusNotFound))
			Expect(fetched).To(HaveLen(1))
		})

		It("refuses a redirect to a host off the allowlist", func() {
			reply = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "coverartarchive.org" {
					resp := respond(302, "", "")
					resp.Header.Set("Location", "https://evil.example/x.jpg")
					return resp, nil
				}
				return respond(200, "image/jpeg", imageBytes), nil
			}
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusNotFound))
			Expect(fetched).To(Equal([]string{
				"https://coverartarchive.org/release-group/" + albumMBID + "/front-500",
			}))
		})

		It("follows a redirect inside the archive's own hosts", func() {
			reply = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "coverartarchive.org" {
					resp := respond(307, "", "")
					resp.Header.Set(
						"Location",
						"https://ia800000.us.archive.org/1/x.jpg",
					)
					return resp, nil
				}
				return respond(200, "image/jpeg", imageBytes), nil
			}
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))
		})

		It("rejects a body that is not an image", func() {
			reply = func(*http.Request) (*http.Response, error) {
				return respond(200, "text/html", "<html>"), nil
			}
			Expect(get("albums", albumMBID).Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("books", func() {
		It("404s a key nothing remembered, without any request", func() {
			Expect(get("books", "42").Code).To(Equal(http.StatusNotFound))
			Expect(fetched).To(BeEmpty())
		})

		It("fetches the image URL a lookup remembered", func() {
			sources.Remember(artwork.KindBooks, "42", artwork.Source{
				URL: "https://assets.hardcover.app/books/42/cover.jpg",
			})
			rec := get("books", "42")

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(fetched).To(Equal([]string{
				"https://assets.hardcover.app/books/42/cover.jpg",
			}))
		})

		It("never fetches a remembered URL on a host outside the allowlist", func() {
			sources.Remember(artwork.KindBooks, "43", artwork.Source{
				URL: "https://evil.example/cover.jpg",
			})
			sources.Remember(artwork.KindBooks, "44", artwork.Source{
				URL: "http://assets.hardcover.app/cover.jpg",
			})
			sources.Remember(artwork.KindBooks, "45", artwork.Source{
				URL: "https://notassets.hardcover.app.evil.example/cover.jpg",
			})
			for _, key := range []string{"43", "44", "45"} {
				Expect(get("books", key).Code).To(Equal(http.StatusNotFound), key)
			}
			Expect(fetched).To(BeEmpty())
		})

		It(
			"serves the library book's own poster for a key already in the library",
			func() {
				lib.ids["books/42"] = 7
				Expect(
					pm.Put(
						context.Background(),
						"books",
						7,
						strings.NewReader("library art"),
					),
				).
					To(Succeed())

				rec := get("books", "42")

				Expect(rec.Code).To(Equal(http.StatusOK))
				Expect(rec.Body.String()).To(Equal("library art"))
				Expect(fetched).To(BeEmpty())
			},
		)

		It(
			"falls through to a fetch when the library row has no poster yet",
			func() {
				lib.ids["books/42"] = 7
				sources.Remember(artwork.KindBooks, "42", artwork.Source{
					URL: "https://assets.hardcover.app/books/42/cover.jpg",
				})
				Expect(get("books", "42").Code).To(Equal(http.StatusOK))
				Expect(fetched).To(HaveLen(1))
			},
		)
	})

	Describe("artists", func() {
		It("404s a key nothing remembered, without any request", func() {
			Expect(get("artists", artistMBID).Code).To(Equal(http.StatusNotFound))
			Expect(fetched).To(BeEmpty())
			Expect(deezer.calls.Load()).To(BeZero())
		})

		It("uses MusicBrainz's own Deezer link first", func() {
			deezer.byID[412] = &metadata.DeezerArtist{
				ID:         412,
				Name:       "Nirvana",
				PictureURL: "https://cdn-images.dzcdn.net/images/artist/x/1000x1000.jpg",
			}
			sources.Remember(artwork.KindArtists, artistMBID, artwork.Source{
				DeezerID: 412, Name: "Nirvana",
			})

			Expect(get("artists", artistMBID).Code).To(Equal(http.StatusOK))
			Expect(fetched).To(Equal([]string{
				"https://cdn-images.dzcdn.net/images/artist/x/1000x1000.jpg",
			}))
		})

		It("accepts a name search with exactly one hit that folds equal", func() {
			deezer.search["Nirvana"] = []metadata.DeezerArtist{
				{
					ID:         1,
					Name:       "NIRVANA",
					PictureURL: "https://cdn-images.dzcdn.net/a.jpg",
				},
				{
					ID:         2,
					Name:       "Nirvana Tribute Band",
					PictureURL: "https://cdn-images.dzcdn.net/b.jpg",
				},
			}
			sources.Remember(
				artwork.KindArtists,
				artistMBID,
				artwork.Source{Name: "Nirvana"},
			)

			Expect(get("artists", artistMBID).Code).To(Equal(http.StatusOK))
			Expect(fetched).To(Equal([]string{"https://cdn-images.dzcdn.net/a.jpg"}))
		})

		It("gives up on a namesake rather than guess a face", func() {
			deezer.search["Nirvana"] = []metadata.DeezerArtist{
				{
					ID:         1,
					Name:       "Nirvana",
					PictureURL: "https://cdn-images.dzcdn.net/a.jpg",
				},
				{
					ID:         2,
					Name:       "Nirvana",
					PictureURL: "https://cdn-images.dzcdn.net/b.jpg",
				},
			}
			sources.Remember(
				artwork.KindArtists,
				artistMBID,
				artwork.Source{Name: "Nirvana"},
			)

			Expect(get("artists", artistMBID).Code).To(Equal(http.StatusNotFound))
			Expect(fetched).To(BeEmpty())
		})
	})

	It("serves a stale copy when the refetch fails", func() {
		Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))
		path := filepath.Join(
			dir,
			"posters",
			"lookup",
			"albums",
			albumMBID,
			"poster.jpg",
		)
		old := fileTime(-48)
		Expect(os.Chtimes(path, old, old)).To(Succeed())
		reply = func(*http.Request) (*http.Response, error) {
			return nil, errors.New("archive down")
		}

		rec := get("albums", albumMBID)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal(imageBytes))
	})

	It("serves a stale copy for a key remembered as failed", func() {
		Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))
		path := filepath.Join(
			dir, "posters", "lookup", "albums", albumMBID, "poster.jpg",
		)
		old := fileTime(-48)
		Expect(os.Chtimes(path, old, old)).To(Succeed())
		reply = func(*http.Request) (*http.Response, error) {
			return respond(404, "text/html", "no"), nil
		}
		Expect(get("albums", albumMBID).Body.String()).To(Equal(imageBytes))

		rec := get("albums", albumMBID)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal(imageBytes))
		Expect(fetched).To(HaveLen(2))
	})

	It("refetches a copy older than a day", func() {
		Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))
		path := filepath.Join(
			dir,
			"posters",
			"lookup",
			"albums",
			albumMBID,
			"poster.jpg",
		)
		old := fileTime(-48)
		Expect(os.Chtimes(path, old, old)).To(Succeed())
		reply = func(*http.Request) (*http.Response, error) {
			return respond(200, "image/jpeg", "newer"), nil
		}

		Expect(get("albums", albumMBID).Body.String()).To(Equal("newer"))
		Expect(fetched).To(HaveLen(2))
	})

	It("keeps the cache bounded, dropping the oldest entries past the cap", func() {
		root := filepath.Join(dir, "posters", "lookup", "books")
		for i := range 2001 {
			d := filepath.Join(root, strconv.Itoa(1000+i))
			Expect(os.MkdirAll(d, 0o750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(d, "poster.jpg"), []byte("x"), 0o600)).
				To(Succeed())
			t := fileTime(-100 + float64(i)/100)
			Expect(os.Chtimes(d, t, t)).To(Succeed())
		}
		Expect(get("albums", albumMBID).Code).To(Equal(http.StatusOK))

		left, err := os.ReadDir(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(left).To(HaveLen(2001 - 200))
		Expect(filepath.Join(root, "1000")).NotTo(BeADirectory())
		Expect(filepath.Join(root, "3000")).To(BeADirectory())
	})
})

var _ = Describe("Sources", Label("unit", "artwork"), func() {
	It("evicts the oldest entry past its capacity", func() {
		s := artwork.NewSources()
		for i := range 2001 {
			s.Remember(artwork.KindBooks, strconv.Itoa(i), artwork.Source{URL: "u"})
		}
		_, ok := s.Lookup(artwork.KindBooks, "0")
		Expect(ok).To(BeFalse())
		_, ok = s.Lookup(artwork.KindBooks, "2000")
		Expect(ok).To(BeTrue())
	})

	It("keeps an entry that was remembered again", func() {
		s := artwork.NewSources()
		s.Remember(artwork.KindBooks, "0", artwork.Source{URL: "u"})
		for i := 1; i <= 1999; i++ {
			s.Remember(artwork.KindBooks, strconv.Itoa(i), artwork.Source{URL: "u"})
		}
		s.Remember(artwork.KindBooks, "0", artwork.Source{URL: "fresh"})
		s.Remember(artwork.KindBooks, "2000", artwork.Source{URL: "u"})

		src, ok := s.Lookup(artwork.KindBooks, "0")
		Expect(ok).To(BeTrue())
		Expect(src.URL).To(Equal("fresh"))
		_, ok = s.Lookup(artwork.KindBooks, "1")
		Expect(ok).To(BeFalse())
	})

	It("ignores an empty source", func() {
		s := artwork.NewSources()
		s.Remember(artwork.KindBooks, "1", artwork.Source{})
		_, ok := s.Lookup(artwork.KindBooks, "1")
		Expect(ok).To(BeFalse())
	})

	It("separates kinds sharing a key", func() {
		s := artwork.NewSources()
		s.Remember(artwork.KindBooks, "1", artwork.Source{URL: "book"})
		_, ok := s.Lookup(artwork.KindArtists, "1")
		Expect(ok).To(BeFalse())
	})
})
