package restapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Handler: Library path migration",
	Label("unit", "server", "library"), func() {
		var (
			app  *apiKeyApp
			root string
		)

		BeforeEach(func() {
			app = newAPIKeyApp()
			root = filepath.Join(GinkgoT().TempDir(), "movies")
			configtest.SetupFile(map[string]any{
				"library": map[string]any{"movie_path": root},
			})
		})

		It("GET reports an idle migration before anything has run", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigration
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Running).To(BeFalse())
			Expect(body.Total).To(BeZero())
		})

		It("GET roots reports each configured root and its tracked count", func() {
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
				}, nil).Once()
			app.store.EXPECT().CountMovieMediaFiles(mock.Anything).
				Return(1, nil).Once()
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			app.store.EXPECT().CountEpisodeMediaFiles(mock.Anything).
				Return(0, nil).Once()
			app.store.EXPECT().
				ListDownloadRecordsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			app.store.EXPECT().
				ListTorrentSessionsByPathPrefix(mock.Anything, mock.Anything).
				Return(nil, nil).Once()
			app.store.EXPECT().CountDownloadRecords(mock.Anything).
				Return(0, nil).Once()
			app.store.EXPECT().CountTorrentSessions(mock.Anything).
				Return(0, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration/roots", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigrationRootList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(3))
			Expect(body.Items[0].Path).To(Equal(root))
			Expect(body.Items[0].Tracked).To(Equal(1))
			Expect(body.Items[0].Total).To(Equal(1))
		})

		It("POST preview counts the rows under the configured root", func() {
			app.store.EXPECT().
				ListMediaFilesByPathPrefix(mock.Anything, root).
				Return([]*ent.MediaFile{
					{ID: 1, Path: filepath.Join(root, "Dune (2021)/d.mkv")},
				}, nil).Once()

			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration/preview", app.adminKey,
				strings.NewReader(`{"root":"movies","to":"/data/movies"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PathMigrationPreview
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.From).To(Equal(root))
			Expect(body.To).To(Equal("/data/movies"))
			Expect(body.Total).To(Equal(1))
			// The file isn't at the destination, so the row would be left alone.
			Expect(body.Skipped).To(Equal(1))
			Expect(body.CanMove).To(BeTrue())
			Expect(body.Samples).To(HaveLen(1))
		})

		It("POST preview rejects a relative destination with 422", func() {
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration/preview", app.adminKey,
				strings.NewReader(`{"root":"movies","to":"movies"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("POST rejects moving files for the download root with 422", func() {
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/path-migration", app.adminKey,
				strings.NewReader(
					`{"root":"downloads","to":"/data/dl","move_files":true}`,
				)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("refuses a non-admin caller", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/path-migration", app.requestOnlyKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
