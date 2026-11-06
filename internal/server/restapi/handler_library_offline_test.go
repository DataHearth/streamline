package restapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/testutil/roottest"
)

var _ = Describe(
	"Handler: an offline library root",
	Label("unit", "server"),
	func() {
		var app *apiKeyApp

		BeforeEach(func() {
			app = newAPIKeyApp()
		})

		expectRefused := func(resp *http.Response) {
			GinkgoHelper()
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			var e Error
			Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
			Expect(e.Code).To(HaveValue(Equal("library_offline")))
			Expect(e.Message).To(ContainSubstring("offline"))
		}
		multipartBody := func() (*bytes.Buffer, string) {
			GinkgoHelper()
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			part, err := mw.CreateFormFile("file", "file.bin")
			Expect(err).NotTo(HaveOccurred())
			_, err = part.Write([]byte("x"))
			Expect(err).NotTo(HaveOccurred())
			Expect(mw.Close()).To(Succeed())
			return &buf, mw.FormDataContentType()
		}

		// The strict mocks fail a spec on any service call, so a 409 from the gate
		// is also proof that the service was never asked.
		DescribeTable(
			"refuses a change to files",
			func(root db.LibraryRoot, method, path string) {
				roottest.Offline(root)
				expectRefused(app.do(app.req(method, path, app.adminKey, nil)))
			},
			Entry(
				"rename movie",
				db.RootMovies,
				http.MethodPost,
				"/api/v1/movies/1/rename",
			),
			Entry(
				"delete movie file",
				db.RootMovies,
				http.MethodDelete,
				"/api/v1/movies/1/files/2",
			),
			Entry(
				"delete movie with files",
				db.RootMovies,
				http.MethodDelete,
				"/api/v1/movies/1?delete_files=true",
			),
			Entry(
				"rename series",
				db.RootSeries,
				http.MethodPost,
				"/api/v1/series/1/rename",
			),
			Entry(
				"delete episode file",
				db.RootSeries,
				http.MethodDelete,
				"/api/v1/series/1/episodes/2/file",
			),
			Entry(
				"delete series with files",
				db.RootSeries,
				http.MethodDelete,
				"/api/v1/series/1?delete_files=true",
			),
			Entry(
				"rename artist",
				db.RootMusic,
				http.MethodPost,
				"/api/v1/music/artists/1/rename",
			),
			Entry(
				"delete track file",
				db.RootMusic,
				http.MethodDelete,
				"/api/v1/music/tracks/1/file",
			),
			Entry(
				"delete artist with files",
				db.RootMusic,
				http.MethodDelete,
				"/api/v1/music/artists/1?delete_files=true",
			),
		)

		DescribeTable(
			"refuses an upload",
			func(root db.LibraryRoot, path string) {
				roottest.Offline(root)
				body, contentType := multipartBody()
				req := app.req(http.MethodPost, path, app.adminKey, body)
				req.Header.Set("Content-Type", contentType)
				expectRefused(app.do(req))
			},
			Entry("album", db.RootMusic, "/api/v1/music/albums/1/upload"),
			Entry("track", db.RootMusic, "/api/v1/music/tracks/1/upload"),
			Entry("artist", db.RootMusic, "/api/v1/music/artists/1/upload"),
			Entry("ebook", db.RootEbooks, "/api/v1/books/1/upload?slot=ebook"),
			Entry(
				"audiobook",
				db.RootAudiobooks,
				"/api/v1/books/1/upload?slot=audiobook",
			),
		)

		It("lets an ebook upload through while only audiobooks are offline", func() {
			roottest.Offline(db.RootAudiobooks)
			app.books.EXPECT().
				UploadBook(mock.Anything, uint32(1), "ebook", mock.Anything, mock.Anything).
				Return(nil, errors.New("reached the service")).Once()
			body, contentType := multipartBody()
			req := app.req(
				http.MethodPost,
				"/api/v1/books/1/upload?slot=ebook",
				app.adminKey,
				body,
			)
			req.Header.Set("Content-Type", contentType)

			resp := app.do(req)
			defer resp.Body.Close()

			Expect(resp.StatusCode).NotTo(Equal(http.StatusConflict))
		})

		DescribeTable(
			"refuses a change to the files of a book held in an offline root",
			func(method, path string) {
				roottest.Offline(db.RootAudiobooks)
				held := []db.LibraryRoot{db.RootEbooks, db.RootAudiobooks}
				app.store.EXPECT().
					BookFileRoots(mock.Anything, uint32(1)).
					Return(held, nil).
					Maybe()
				app.store.EXPECT().
					BookSeriesFileRoots(mock.Anything, uint32(1)).
					Return(held, nil).
					Maybe()

				expectRefused(app.do(app.req(method, path, app.adminKey, nil)))
			},
			Entry("rename book", http.MethodPost, "/api/v1/books/1/rename"),
			Entry(
				"rename book series",
				http.MethodPost,
				"/api/v1/books/series/1/rename",
			),
			Entry(
				"delete book with files",
				http.MethodDelete,
				"/api/v1/books/1?delete_files=true",
			),
			Entry(
				"delete book series with files",
				http.MethodDelete,
				"/api/v1/books/series/1?delete_files=true",
			),
		)

		It("deletes an audiobook-only book while only ebooks are offline", func() {
			roottest.Offline(db.RootEbooks)
			app.store.EXPECT().BookFileRoots(mock.Anything, uint32(1)).
				Return([]db.LibraryRoot{db.RootAudiobooks}, nil).Once()
			app.books.EXPECT().
				DeleteBook(mock.Anything, uint32(1), true).
				Return(nil).
				Once()

			resp := app.do(
				app.req(
					http.MethodDelete,
					"/api/v1/books/1?delete_files=true",
					app.adminKey,
					nil,
				),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		DescribeTable(
			"refuses a re-identify, which renames files",
			func(root db.LibraryRoot, path, body string) {
				roottest.Offline(root)
				req := app.req(
					http.MethodPost,
					path,
					app.adminKey,
					strings.NewReader(body),
				)
				req.Header.Set("Content-Type", "application/json")

				expectRefused(app.do(req))
			},
			Entry(
				"movie",
				db.RootMovies,
				"/api/v1/movies/1/reidentify",
				`{"tmdb_id":1}`,
			),
			Entry(
				"series",
				db.RootSeries,
				"/api/v1/series/1/reidentify",
				`{"tvdb_id":1}`,
			),
		)

		It("still deletes a movie that keeps its files", func() {
			roottest.Offline(db.RootMovies)
			app.movies.EXPECT().
				Delete(mock.Anything, uint32(1), mock.Anything).
				Return(nil).
				Once()

			resp := app.do(
				app.req(http.MethodDelete, "/api/v1/movies/1", app.adminKey, nil),
			)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("answers a bulk import commit the service refused", func() {
			app.bulkImports.EXPECT().
				CommitWith(mock.Anything, uint32(1), mock.Anything).
				Return(&roothealth.OfflineError{Root: db.RootMusic}).
				Once()

			expectRefused(app.do(app.req(
				http.MethodPost,
				"/api/v1/library/imports/1/commit",
				app.adminKey,
				nil,
			)))
		})

		It("keeps an unrelated service failure opaque", func() {
			app.bulkImports.EXPECT().
				CommitWith(mock.Anything, uint32(1), mock.Anything).
				Return(bulkimport.ErrUnsupportedKind).Once()

			resp := app.do(app.req(
				http.MethodPost,
				"/api/v1/library/imports/1/commit",
				app.adminKey,
				nil,
			))
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})
	},
)
