package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func fixture(name string) []byte {
	GinkgoHelper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	Expect(err).NotTo(HaveOccurred())
	return b
}

var _ = Describe("Client", Label("unit", "arr"), func() {
	var (
		srv     *httptest.Server
		keys    []string
		queries []string
	)

	newServer := func(routes map[string]string) *httptest.Server {
		GinkgoHelper()
		return httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				keys = append(keys, r.Header.Get("X-Api-Key"))
				queries = append(queries, r.URL.RawQuery)
				body, ok := routes[r.URL.Path]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write(fixture(body))
				Expect(err).NotTo(HaveOccurred())
			}),
		)
	}

	AfterEach(func() {
		if srv != nil {
			srv.Close()
			srv = nil
		}
		keys, queries = nil, nil
	})

	It("sends the api key as X-Api-Key and decodes the movie list", func() {
		srv = newServer(map[string]string{
			"/api/v3/movie": "radarr_movie.json",
		})
		c, err := NewFactory().Client(Radarr, srv.URL, "secret-key")
		Expect(err).NotTo(HaveOccurred())

		movies, err := c.Movies(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(keys).To(ConsistOf("secret-key"))
		Expect(movies).To(HaveLen(2))
		Expect(movies[0].TMDBID).To(Equal(uint32(27205)))
		Expect(movies[0].MovieFile).NotTo(BeNil())
		Expect(movies[0].MovieFile.Path).
			To(Equal("/media/movies/Inception (2010)/Inception.2010.1080p.mkv"))
		Expect(movies[1].HasFile).To(BeFalse())
		Expect(movies[1].MovieFile).To(BeNil())
	})

	It("asks for the embedded episode file and decodes it", func() {
		srv = newServer(map[string]string{
			"/api/v3/episode": "sonarr_episode.json",
		})
		c, err := NewFactory().Client(Sonarr, srv.URL+"/", "k")
		Expect(err).NotTo(HaveOccurred())

		eps, err := c.Episodes(context.Background(), 7)
		Expect(err).NotTo(HaveOccurred())
		Expect(queries).To(ConsistOf("seriesId=7&includeEpisodeFile=true"))
		Expect(eps).To(HaveLen(3))
		Expect(eps[0].EpisodeFile).NotTo(BeNil())
		Expect(eps[0].EpisodeFile.Size).To(Equal(int64(2147483648)))
		Expect(eps[1].EpisodeFile).To(BeNil())
	})

	It("accepts the application it was built for", func() {
		srv = newServer(map[string]string{
			"/api/v3/system/status": "status_sonarr.json",
		})
		c, err := NewFactory().Client(Sonarr, srv.URL, "k")
		Expect(err).NotTo(HaveOccurred())

		Expect(c.TestConnection(context.Background())).To(Succeed())
	})

	It("reports a Sonarr answering a Radarr client as the wrong app", func() {
		srv = newServer(map[string]string{
			"/api/v3/system/status": "status_sonarr.json",
		})
		c, err := NewFactory().Client(Radarr, srv.URL, "k")
		Expect(err).NotTo(HaveOccurred())

		Expect(c.TestConnection(context.Background())).
			To(MatchError(ErrWrongApp))
	})

	It("maps a 401 onto ErrUnauthorized", func() {
		srv = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}),
		)
		c, err := NewFactory().Client(Sonarr, srv.URL, "bad")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.Series(context.Background())
		Expect(err).To(MatchError(ErrUnauthorized))
	})

	It("maps an unreachable host onto ErrUnreachable without the URL", func() {
		srv = httptest.NewServer(http.NotFoundHandler())
		base := srv.URL
		srv.Close()
		srv = nil

		c, err := NewFactory().Client(Radarr, base, "k")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.Movies(context.Background())
		Expect(err).To(MatchError(ErrUnreachable))
		Expect(err.Error()).NotTo(ContainSubstring(base))
	})

	It("rejects a base URL that is not http or https", func() {
		_, err := NewFactory().Client(Radarr, "ftp://host", "k")
		Expect(err).To(MatchError(ErrInvalidURL))
	})

	It("rejects a base URL with no host", func() {
		_, err := NewFactory().Client(Radarr, "http://", "k")
		Expect(err).To(MatchError(ErrInvalidURL))
	})
})
