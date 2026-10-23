package restapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
)

var _ = Describe("roleGuard", Label("unit"), func() {
	// invoke runs op through the guard as role ("" = unauthenticated) and
	// reports the recorded response plus whether the handler behind the guard
	// was reached.
	invoke := func(op, role string) (*httptest.ResponseRecorder, bool) {
		GinkgoHelper()
		reached := false
		guarded := roleGuard(func(
			context.Context, http.ResponseWriter, *http.Request, any,
		) (any, error) {
			reached = true
			return nil, nil
		}, op)

		ctx := context.Background()
		if role != "" {
			ctx = auth.ContextWithClaims(ctx, &auth.Claims{Role: role})
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err := guarded(ctx, rec, req, nil)
		Expect(err).NotTo(HaveOccurred())
		return rec, reached
	}

	It("lists every operation of StrictServerInterface exactly once", func() {
		iface := reflect.TypeFor[StrictServerInterface]()
		exists := make(map[string]bool, iface.NumMethod())
		var missing []string
		for m := range iface.Methods() {
			exists[m.Name] = true
			if _, ok := minRole[m.Name]; !ok {
				missing = append(missing, m.Name)
			}
		}
		Expect(missing).To(BeEmpty(), "unlisted operations are denied at runtime")

		var stale []string
		for op := range minRole {
			if !exists[op] {
				stale = append(stale, op)
			}
		}
		Expect(stale).To(BeEmpty(), "minRole names operations that no longer exist")
	})

	DescribeTable(
		"enforces the minimum role",
		func(op, role string, allow bool) {
			rec, reached := invoke(op, role)
			Expect(reached).To(Equal(allow))
			if !allow {
				Expect(rec.Code).To(Equal(http.StatusForbidden))
				Expect(
					rec.Header().Get("Content-Type"),
				).To(Equal("application/json"))
				Expect(rec.Body.String()).To(ContainSubstring("role required"))
			}
		},
		Entry(
			"request_only cannot grab",
			"GrabMovieRelease",
			roleRequestOnly,
			false,
		),
		Entry("member can grab", "GrabMovieRelease", roleMember, true),
		Entry("admin can grab", "GrabMovieRelease", roleAdmin, true),
		Entry(
			"request_only cannot delete a movie",
			"DeleteMovie",
			roleRequestOnly,
			false,
		),
		Entry(
			"request_only cannot rename",
			"RenameSeriesFiles",
			roleRequestOnly,
			false,
		),
		Entry(
			"request_only cannot browse releases",
			"BrowseSeasonReleases",
			roleRequestOnly,
			false,
		),
		Entry("member cannot list users", "ListUsers", roleMember, false),
		Entry(
			"request_only cannot read download clients",
			"ListDownloadClients",
			roleRequestOnly,
			false,
		),
		Entry(
			"member cannot read download clients",
			"ListDownloadClients",
			roleMember,
			false,
		),
		Entry(
			"request_only cannot read indexers",
			"ListIndexers",
			roleRequestOnly,
			false,
		),
		Entry("member cannot read indexers", "ListIndexers", roleMember, false),
		Entry(
			"request_only cannot list media servers",
			"ListMediaServers",
			roleRequestOnly,
			false,
		),
		Entry(
			"request_only cannot read a media server",
			"GetMediaServer",
			roleRequestOnly,
			false,
		),
		Entry(
			"member cannot read a media server",
			"GetMediaServer",
			roleMember,
			false,
		),
		Entry(
			"request_only cannot list torrents",
			"ListTorrents",
			roleRequestOnly,
			false,
		),
		Entry(
			"request_only cannot read torrent peers",
			"GetTorrent",
			roleRequestOnly,
			false,
		),
		Entry("member cannot read torrent peers", "GetTorrent", roleMember, false),
		Entry(
			"request_only cannot read movie play-on links",
			"GetMoviePlayOnLinks",
			roleRequestOnly,
			false,
		),
		Entry(
			"member can read movie play-on links",
			"GetMoviePlayOnLinks",
			roleMember,
			true,
		),
		Entry(
			"request_only cannot read series play-on links",
			"GetSeriesPlayOnLinks",
			roleRequestOnly,
			false,
		),
		Entry("request_only may request", "CreateRequest", roleRequestOnly, true),
		Entry("request_only may read its account", "AuthMe", roleRequestOnly, true),
		Entry("an unknown role is rejected", "ListMovies", "superuser", false),
	)

	It("401s an unauthenticated caller", func() {
		rec, reached := invoke("ListMovies", "")
		Expect(reached).To(BeFalse())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("denies an operation that is absent from the table", func() {
		rec, reached := invoke("SomeNewlyGeneratedOperation", roleAdmin)
		Expect(reached).To(BeFalse())
		Expect(rec.Code).To(Equal(http.StatusForbidden))
	})
})
