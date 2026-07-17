package restapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/internal/auth"
	authmocks "github.com/datahearth/streamline/internal/auth/mocks"
	bittorrentmocks "github.com/datahearth/streamline/internal/bittorrent/mocks"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	downloadmocks "github.com/datahearth/streamline/internal/download/mocks"
	indexermocks "github.com/datahearth/streamline/internal/indexer/mocks"
	moviemocks "github.com/datahearth/streamline/internal/media/movie/mocks"
	tvshowmocks "github.com/datahearth/streamline/internal/media/tvshow/mocks"
	mediaservermocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	metadatamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	reqmocks "github.com/datahearth/streamline/internal/request/mocks"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestRestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RestAPI Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

// Seed the config singleton with sane defaults for every spec.
var _ = BeforeEach(func() {
	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "test-secret-key-for-jwt",
			"session_ttl":    "1h",
		},
		"metadata": map[string]any{
			"tmdb_api_key": "test-key",
		},
	})
})

// apiKeyApp is the restapi unit-test harness: a chi router mounted with
// restapi.Mount + a tiny identity-injecting middleware. Every service in
// restapi.Deps is a mockery mock; tests set EXPECT() per spec. There is no
// database — the harness exists so handlers can be exercised over real HTTP
// without ent, sqlite, or any real auth.Service.
//
// Identity is selected per-request via the X-API-Key header. The two
// well-known values seeded at construction time are `adminKey` and (when
// addMember is called) `memberKey`. Any other value or an empty header
// produces an unauthenticated request.
type apiKeyApp struct {
	srv *httptest.Server

	// Mocks. Tests set expectations directly on these.
	auth         *authmocks.MockManager
	movies       *moviemocks.MockManager
	metadata     *metadatamocks.MockProvider
	indexers     *indexermocks.MockManager
	downloads    *downloadmocks.MockDownloader
	mediaServers *mediaservermocks.MockManager
	tvshows      *tvshowmocks.MockManager
	metadataTV   *metadatamocks.MockTVProvider
	requests     *reqmocks.MockManager
	torrents     *bittorrentmocks.MockManager
	store        *dbmocks.MockStore

	// Identity tokens consumed by the synthetic auth middleware.
	adminKey       string
	memberKey      string
	requestOnlyKey string
	adminID        uint32
	memberID       uint32
	requestOnlyID  uint32
}

// newAPIKeyApp constructs the test harness. The caller receives cleanup
// registered via DeferCleanup.
func newAPIKeyApp() *apiKeyApp {
	GinkgoHelper()
	t := GinkgoT()

	a := &apiKeyApp{
		auth:           authmocks.NewMockManager(t),
		movies:         moviemocks.NewMockManager(t),
		metadata:       metadatamocks.NewMockProvider(t),
		indexers:       indexermocks.NewMockManager(t),
		downloads:      downloadmocks.NewMockDownloader(t),
		mediaServers:   mediaservermocks.NewMockManager(t),
		tvshows:        tvshowmocks.NewMockManager(t),
		metadataTV:     metadatamocks.NewMockTVProvider(t),
		requests:       reqmocks.NewMockManager(t),
		torrents:       bittorrentmocks.NewMockManager(t),
		store:          dbmocks.NewMockStore(t),
		adminKey:       "test-admin-token",
		adminID:        1,
		requestOnlyKey: "test-requestonly-token",
		requestOnlyID:  3,
	}

	rsrv := New(Deps{
		Auth:         a.auth,
		Movies:       a.movies,
		Metadata:     a.metadata,
		Indexers:     a.indexers,
		Downloads:    a.downloads,
		MediaServers: a.mediaServers,
		TVShows:      a.tvshows,
		MetadataTV:   a.metadataTV,
		Requests:     a.requests,
		Torrents:     a.torrents,
		Store:        a.store,
	})

	r := chi.NewRouter()
	r.Use(a.identityMiddleware())
	Mount(r, rsrv)

	ts := httptest.NewServer(r)
	DeferCleanup(ts.Close)
	a.srv = ts
	return a
}

// identityMiddleware mimics what the real auth middleware places on the
// request context on success: an *auth.Claims with the caller's UserID,
// Email, Role, and JTI. The handler-level requireAdmin/ClaimsFromContext
// checks therefore see exactly the same shape they see in production.
func (a *apiKeyApp) identityMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			var claims *auth.Claims
			switch {
			case key == "" || key == a.adminKey:
				// Default test identity: an authenticated admin. Most domain
				// tests assume an authenticated admin session; tests exercising
				// RBAC pass an explicit member/request-only key, and no-auth
				// (401) tests pass an explicit unknown token.
				claims = &auth.Claims{
					UserID: a.adminID,
					Email:  "admin@test.com",
					Role:   "admin",
					JTI:    "admin-jti",
				}
			case a.memberKey != "" && key == a.memberKey:
				claims = &auth.Claims{
					UserID: a.memberID,
					Email:  "member@test.com",
					Role:   "member",
					JTI:    "member-jti",
				}
			case key == a.requestOnlyKey:
				claims = &auth.Claims{
					UserID: a.requestOnlyID,
					Email:  "requestonly@test.com",
					Role:   "request_only",
					JTI:    "requestonly-jti",
				}
			}
			if claims != nil {
				ctx := auth.ContextWithClaims(r.Context(), claims)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// An explicit unknown token → no claims attached. Handlers that gate
			// on claims return 401/403 themselves; handlers that don't run
			// normally.
			next.ServeHTTP(w, r)
		})
	}
}

// addMember registers a non-admin identity. The token is returned and also
// stored on a.memberKey. Pass the resulting key in subsequent requests to
// authenticate as the member.
func (a *apiKeyApp) addMember(_ string) {
	GinkgoHelper()
	a.memberID = 2
	a.memberKey = "test-member-token"
}

// req builds a request against the test server with the given API key set.
// Passing an empty key produces an unauthenticated request.
func (a *apiKeyApp) req(method, path, apiKey string, body io.Reader) *http.Request {
	GinkgoHelper()
	req, err := http.NewRequest(method, a.srv.URL+path, body)
	Expect(err).NotTo(HaveOccurred())
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req
}

// do executes req through the default client.
func (a *apiKeyApp) do(req *http.Request) *http.Response {
	GinkgoHelper()
	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}
