package restapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe(
	"Download Client Integration (testcontainers)",
	Label("integration", "downloads", "containers"),
	Ordered,
	func() {
		var (
			qbtContainer testcontainers.Container
			qbtHost      string
			qbtPort      uint16
			ts           *httptest.Server
			dbClient     *ent.Client
		)

		BeforeAll(func() {
			By("Starting qBittorrent container")
			ctx := context.Background()
			req := testcontainers.ContainerRequest{
				Image:        "linuxserver/qbittorrent:4.6.7",
				ExposedPorts: []string{"8080/tcp"},
				Env: map[string]string{
					"PUID":       "1000",
					"PGID":       "1000",
					"WEBUI_PORT": "8080",
				},
				WaitingFor: wait.ForListeningPort("8080/tcp").
					WithStartupTimeout(3 * time.Minute),
			}

			var err error
			qbtContainer, err = testcontainers.GenericContainer(
				ctx,
				testcontainers.GenericContainerRequest{
					ContainerRequest: req,
					Started:          true,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = qbtContainer.Terminate(context.Background())
			})

			host, err := qbtContainer.Host(ctx)
			Expect(err).NotTo(HaveOccurred())
			mappedPort, err := qbtContainer.MappedPort(ctx, "8080")
			Expect(err).NotTo(HaveOccurred())
			port, err := strconv.ParseUint(mappedPort.Port(), 10, 16)
			Expect(err).NotTo(HaveOccurred())
			qbtHost = host
			qbtPort = uint16(port)

			By("Setting up in-memory DB + test server")
			dbClient = dbtest.SetupTestDB(ctx)
			DeferCleanup(func() { dbClient.Close() })

			store := db.New(dbClient)
			srv := New(Deps{
				Downloads: download.New(store),
			})
			r := chi.NewRouter()
			// Inject an admin identity so requireAdmin-gated CRUD handlers run.
			r.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(
					func(w http.ResponseWriter, req *http.Request) {
						next.ServeHTTP(w, req.WithContext(auth.ContextWithClaims(
							req.Context(), &auth.Claims{
								UserID: 1, Email: "admin@test.com",
								Role: "admin", JTI: "admin-jti",
							}),
						))
					},
				)
			})
			Mount(r, srv)
			ts = httptest.NewServer(r)
			DeferCleanup(ts.Close)
		})

		// Download clients live in the config singleton post-refactor, so the
		// CRUD handlers mutate config via config.Update — which needs a
		// file-backed config to write back to. configtest.SetupFile provides
		// one; it runs after the suite-level reader-based BeforeEach, so the
		// file-backed config wins. The singleton is reset per spec, so the
		// round-trip lives in a single It rather than sharing state across an
		// Ordered pair.
		BeforeEach(func() {
			configtest.SetupFile()
		})

		It(
			"creates a download client pointing at the real qBittorrent container and lists it back",
			func() {
				body := fmt.Sprintf(
					`{"name":"qbt-real","client_type":"qbittorrent","host":%q,"port":%d,"username":"admin","password":"adminadmin"}`,
					qbtHost,
					qbtPort,
				)
				resp, err := http.Post(
					ts.URL+"/api/v1/download-clients",
					"application/json",
					strings.NewReader(body),
				)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				listResp, err := http.Get(ts.URL + "/api/v1/download-clients")
				Expect(err).NotTo(HaveOccurred())
				defer listResp.Body.Close()
				Expect(listResp.StatusCode).To(Equal(http.StatusOK))

				var list []DownloadClient
				Expect(json.NewDecoder(listResp.Body).Decode(&list)).To(Succeed())
				Expect(list).To(HaveLen(1))
				Expect(list[0].Name).To(Equal("qbt-real"))
			},
		)
	},
)
