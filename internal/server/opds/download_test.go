package opds

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent/mediafile"
)

var _ = g.Describe("download", g.Label("integration"), func() {
	var f *catalogFixture

	g.BeforeEach(func(ctx g.SpecContext) {
		f = newCatalogFixture(ctx)
	})

	g.It(
		"serves the whole file with its content type and a sanitized filename",
		func() {
			rec := f.get(downloadPath(f.ebook.ID, "epub"), true)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(Equal("EPUBDATA"))
			Expect(
				rec.Header().Get("Content-Type"),
			).To(Equal("application/epub+zip"))
			Expect(
				rec.Header().Get("Content-Disposition"),
			).To(Equal(`attachment; filename="Elantris - Brandon Sanderson.epub"`))
		},
	)

	g.It("honours Range requests", func() {
		req := httptest.NewRequest(
			http.MethodGet,
			downloadPath(f.ebook.ID, "epub"),
			nil,
		)
		req.SetBasicAuth(testEmail, "sesame")
		req.Header.Set("Range", "bytes=0-3")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusPartialContent))
		Expect(rec.Body.String()).To(Equal("EPUB"))
	})

	g.It(
		"answers 404 for an unknown book, a missing format or an audiobook",
		func() {
			Expect(
				f.get(downloadPath(99999, "epub"), true).Code,
			).To(Equal(http.StatusNotFound))
			Expect(
				f.get("/opds/download/abc/epub", true).Code,
			).To(Equal(http.StatusNotFound))
			Expect(
				f.get(downloadPath(f.ebook.ID, "pdf"), true).Code,
			).To(Equal(http.StatusNotFound))
			Expect(
				f.get(downloadPath(f.audioOnly.ID, "m4b"), true).Code,
			).To(Equal(http.StatusNotFound))
		},
	)

	g.It("answers 404 when the file is gone from disk", func() {
		Expect(os.Remove(f.file)).To(Succeed())
		Expect(
			f.get(downloadPath(f.ebook.ID, "epub"), true).Code,
		).To(Equal(http.StatusNotFound))
	})

	g.It(
		"falls back to the file extension for the other format",
		func(ctx g.SpecContext) {
			path := filepath.Join(g.GinkgoT().TempDir(), "elantris.djvu")
			Expect(os.WriteFile(path, []byte("DJVU"), 0o600)).To(Succeed())
			f.client.MediaFile.Create().SetPath(path).SetSize(4).SetQuality("other").
				SetBookKind(mediafile.BookKindEbook).SetBook(f.ebook).SaveX(ctx)

			rec := f.get(downloadPath(f.ebook.ID, "other"), true)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(
				rec.Header().Get("Content-Type"),
			).To(Equal("application/octet-stream"))
			Expect(
				rec.Header().Get("Content-Disposition"),
			).To(ContainSubstring(`Elantris - Brandon Sanderson.djvu"`))
		},
	)

	g.It(
		"keeps the filename well-formed when the extension is hostile",
		func(ctx g.SpecContext) {
			path := filepath.Join(g.GinkgoT().TempDir(), `elantris.e"; x`)
			Expect(os.WriteFile(path, []byte("X"), 0o600)).To(Succeed())
			f.client.MediaFile.Create().SetPath(path).SetSize(1).SetQuality("other").
				SetBookKind(mediafile.BookKindEbook).SetBook(f.ebook).SaveX(ctx)

			rec := f.get(downloadPath(f.ebook.ID, "other"), true)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Disposition")).
				To(MatchRegexp(`^attachment; filename="[^"]*"$`))
		},
	)

	g.It("requires authentication", func() {
		Expect(
			f.get(downloadPath(f.ebook.ID, "epub"), false).Code,
		).To(Equal(http.StatusUnauthorized))
	})
})

func downloadPath(bookID uint32, format string) string {
	return fmt.Sprintf("/opds/download/%d/%s", bookID, format)
}
