package restapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/upload"
)

var _ = Describe("Handler: uploads", Label("unit", "server"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("")
	})

	postUpload := func(path string) *http.Response {
		GinkgoHelper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("file", "01.flac")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write([]byte("x"))
		Expect(err).NotTo(HaveOccurred())
		Expect(mw.Close()).To(Succeed())
		req := app.req(http.MethodPost, path, app.memberKey, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return app.do(req)
	}
	errorOf := func(resp *http.Response) Error {
		GinkgoHelper()
		defer resp.Body.Close()
		var e Error
		Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
		return e
	}

	It("answers a busy album with 409 upload_conflict", func() {
		app.music.EXPECT().
			UploadAlbum(mock.Anything, uint32(7), mock.Anything, mock.Anything).
			Return(nil, upload.ErrConflict).
			Once()

		resp := postUpload("/api/v1/music/albums/7/upload")

		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		Expect(*errorOf(resp).Code).To(Equal("upload_conflict"))
	})

	It("answers a busy album from the preview with 409 upload_conflict", func() {
		app.music.EXPECT().
			AlbumUploadPreview(mock.Anything, uint32(7), mock.Anything).
			Return(nil, upload.ErrConflict).
			Once()

		resp := app.do(app.req(
			http.MethodPost, "/api/v1/music/albums/7/upload/preview", app.memberKey,
			strings.NewReader(`{"files":[{"path":"01.flac","size":1}]}`),
		))

		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		Expect(*errorOf(resp).Code).To(Equal("upload_conflict"))
	})

	It("answers a refused preview with 422 upload_rejected and the reason", func() {
		app.music.EXPECT().
			AlbumUploadPreview(mock.Anything, uint32(7), mock.Anything).
			Return(nil, fmt.Errorf("%w: bonus.mkv", upload.ErrRejected)).
			Once()

		resp := app.do(app.req(
			http.MethodPost, "/api/v1/music/albums/7/upload/preview", app.memberKey,
			strings.NewReader(`{"files":[{"path":"bonus.mkv","size":1}]}`),
		))

		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		e := errorOf(resp)
		Expect(*e.Code).To(Equal("upload_rejected"))
		Expect(e.Message).To(ContainSubstring("bonus.mkv"))
	})

	It("answers a rejected upload with 422 and the reason", func() {
		app.music.EXPECT().
			UploadAlbum(mock.Anything, uint32(7), mock.Anything, mock.Anything).
			Return(nil, fmt.Errorf("%w: bad name ../x", upload.ErrRejected)).
			Once()

		resp := postUpload("/api/v1/music/albums/7/upload")

		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		e := errorOf(resp)
		Expect(*e.Code).To(Equal("upload_rejected"))
		Expect(e.Message).To(ContainSubstring("bad name ../x"))
	})

	It("answers 202 with the record id and enqueues the import once", func() {
		app.music.EXPECT().
			UploadAlbum(mock.Anything, uint32(7), mock.Anything, mock.Anything).
			Return(&ent.DownloadRecord{ID: 42}, nil).
			Once()
		app.importer.EXPECT().Enqueue(uint32(42)).Once()

		resp := postUpload("/api/v1/music/albums/7/upload")

		Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		defer resp.Body.Close()
		var out UploadAccepted
		Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
		Expect(out.RecordId).To(Equal(uint32(42)))
	})

	It("lists the folders an artist upload skipped", func() {
		app.music.EXPECT().
			UploadArtist(mock.Anything, uint32(3), mock.Anything, mock.Anything).
			Return(&ent.DownloadRecord{ID: 5}, []string{"Bootlegs"}, nil).
			Once()
		app.importer.EXPECT().Enqueue(uint32(5)).Once()

		resp := postUpload("/api/v1/music/artists/3/upload")

		Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		defer resp.Body.Close()
		var out UploadAccepted
		Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
		Expect(*out.SkippedFolders).To(ConsistOf("Bootlegs"))
	})

	It("refuses a request-only caller", func() {
		resp := func() *http.Response {
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			Expect(mw.Close()).To(Succeed())
			req := app.req(
				http.MethodPost, "/api/v1/music/albums/7/upload",
				app.requestOnlyKey, &buf,
			)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			return app.do(req)
		}()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("refuses a preview of more than 5000 files", func() {
		files := make([]UploadPreviewFile, 5001)
		for i := range files {
			files[i] = UploadPreviewFile{Path: fmt.Sprintf("%d.flac", i), Size: 1}
		}
		body, err := json.Marshal(UploadPreviewRequest{Files: files})
		Expect(err).NotTo(HaveOccurred())
		req := app.req(
			http.MethodPost, "/api/v1/music/albums/7/upload/preview",
			app.memberKey, bytes.NewReader(body),
		)
		req.Header.Set("Content-Type", "application/json")

		resp := app.do(req)

		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		Expect(*errorOf(resp).Code).To(Equal("upload_rejected"))
	})
})
