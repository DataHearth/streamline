package opds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

const testEmail = "reader@example.com"

type catalogFixture struct {
	client    *ent.Client
	router    http.Handler
	author    *ent.Author
	ebook     *ent.Book
	audioOnly *ent.Book
	file      string
}

// newBook creates a book credited to a, with the role as its contribution.
func newBook(
	ctx context.Context,
	client *ent.Client,
	hc uint32,
	title string,
	a *ent.Author,
	role bookcontribution.Role,
) *ent.Book {
	g.GinkgoHelper()
	b := client.Book.Create().
		SetHardcoverID(hc).
		SetTitle(title).
		SetAuthorName(a.Name).
		SaveX(ctx)
	client.BookContribution.Create().
		SetBook(b).SetAuthor(a).SetRole(role).SaveX(ctx)
	return b
}

func newCatalogFixture(ctx context.Context) *catalogFixture {
	g.GinkgoHelper()
	client := dbtest.SetupTestDB(ctx)
	g.DeferCleanup(client.Close)

	a := client.Author.Create().
		SetHardcoverID(1).
		SetName("Brandon Sanderson").
		SetSortName("Sanderson, Brandon").
		SaveX(ctx)
	ebook := newBook(ctx, client, 10, "Elantris", a, bookcontribution.RoleAuthor)
	client.Book.UpdateOne(ebook).SetOverview("A fallen city.").ExecX(ctx)
	audioOnly := newBook(
		ctx,
		client,
		11,
		"Warbreaker",
		a,
		bookcontribution.RoleAuthor,
	)

	file := filepath.Join(g.GinkgoT().TempDir(), "elantris.epub")
	Expect(os.WriteFile(file, []byte("EPUBDATA"), 0o600)).To(Succeed())
	client.MediaFile.Create().SetPath(file).SetSize(8).SetQuality("EPUB").
		SetBookKind(mediafile.BookKindEbook).SetBook(ebook).SaveX(ctx)
	client.MediaFile.Create().
		SetPath(filepath.Join(filepath.Dir(file), "warbreaker.m4b")).
		SetSize(1).
		SetQuality("M4B").
		SetBookKind(mediafile.BookKindAudiobook).
		SetBook(audioOnly).
		SaveX(ctx)

	client.User.Create().
		SetEmail(testEmail).
		SetOpdsToken(auth.HashOPDSToken("sesame")).
		SaveX(ctx)

	root := chi.NewRouter()
	root.Mount("/opds", New(client, appaccess.NewTracker(client)).Router())
	return &catalogFixture{
		client:    client,
		router:    root,
		author:    a,
		ebook:     ebook,
		audioOnly: audioOnly,
		file:      file,
	}
}

func (f *catalogFixture) get(path string, auth bool) *httptest.ResponseRecorder {
	g.GinkgoHelper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth {
		req.SetBasicAuth(testEmail, "sesame")
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

var _ = g.Describe("catalog", g.Label("integration"), func() {
	var f *catalogFixture

	g.BeforeEach(func(ctx g.SpecContext) {
		f = newCatalogFixture(ctx)
	})

	g.It("serves a root navigation feed with a search link", func() {
		rec := f.get("/opds", true)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(
			rec.Header().Get("Content-Type"),
		).To(ContainSubstring("kind=navigation"))
		body := rec.Body.String()
		Expect(body).To(ContainSubstring("<title>Authors</title>"))
		Expect(body).To(ContainSubstring("<title>Recently added</title>"))
		Expect(body).To(ContainSubstring(`rel="search"`))
		Expect(
			body,
		).To(ContainSubstring(`type="application/opensearchdescription+xml"`))
		Expect(body).To(ContainSubstring(`href="/opds/search.xml"`))
	})

	g.It(
		"lists authors with ebooks, ordered by sort name falling back to name",
		func(ctx g.SpecContext) {
			zed := f.client.Author.Create().
				SetHardcoverID(2).
				SetName("Aaron Zed").
				SaveX(ctx)
			b := newBook(
				ctx,
				f.client,
				12,
				"Zed Book",
				zed,
				bookcontribution.RoleWriter,
			)
			f.client.MediaFile.Create().
				SetPath("/x/zed.epub").
				SetSize(1).
				SetQuality("EPUB").
				SetBookKind(mediafile.BookKindEbook).
				SetBook(b).
				SaveX(ctx)
			audioAuthor := f.client.Author.Create().
				SetHardcoverID(3).
				SetName("Audio Only").
				SaveX(ctx)
			ab := newBook(
				ctx,
				f.client,
				13,
				"Spoken",
				audioAuthor,
				bookcontribution.RoleAuthor,
			)
			f.client.MediaFile.Create().
				SetPath("/x/spoken.m4b").
				SetSize(1).
				SetQuality("M4B").
				SetBookKind(mediafile.BookKindAudiobook).
				SetBook(ab).
				SaveX(ctx)

			rec := f.get("/opds/authors", true)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(
				rec.Header().Get("Content-Type"),
			).To(ContainSubstring("kind=navigation"))
			body := rec.Body.String()
			Expect(
				body,
			).To(ContainSubstring(fmt.Sprintf(`href="/opds/authors/%d"`, f.author.ID)))
			Expect(body).NotTo(ContainSubstring("Audio Only"))
			Expect(body).To(MatchRegexp(`(?s)Aaron Zed.*Brandon Sanderson`))
			Expect(
				strings.Index(body, "Aaron Zed"),
			).To(BeNumerically("<", strings.Index(body, "Brandon Sanderson")))
		},
	)

	g.It("serves an acquisition feed for an author's ebook-available books", func() {
		rec := f.get(fmt.Sprintf("/opds/authors/%d", f.author.ID), true)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(
			rec.Header().Get("Content-Type"),
		).To(ContainSubstring("kind=acquisition"))
		body := rec.Body.String()
		Expect(body).To(ContainSubstring("Elantris"))
		Expect(
			body,
		).To(ContainSubstring(fmt.Sprintf(`href="/opds/download/%d/epub"`, f.ebook.ID)))
		Expect(body).To(ContainSubstring(`type="application/epub+zip"`))
		Expect(
			body,
		).To(ContainSubstring(fmt.Sprintf(`href="/posters/books/%d/poster.jpg"`, f.ebook.ID)))
		Expect(body).To(ContainSubstring("A fallen city."))
		Expect(body).NotTo(ContainSubstring("Warbreaker"))
	})

	g.It("answers 404 for a bad or unknown author id", func() {
		Expect(f.get("/opds/authors/abc", true).Code).To(Equal(http.StatusNotFound))
		Expect(
			f.get("/opds/authors/99999", true).Code,
		).To(Equal(http.StatusNotFound))
	})

	g.It(
		"serves recent ebook-available books newest first",
		func(ctx g.SpecContext) {
			later := newBook(
				ctx,
				f.client,
				14,
				"Mistborn",
				f.author,
				bookcontribution.RoleAuthor,
			)
			f.client.MediaFile.Create().
				SetPath("/x/mistborn.epub").
				SetSize(1).
				SetQuality("EPUB").
				SetBookKind(mediafile.BookKindEbook).
				SetBook(later).
				SaveX(ctx)

			rec := f.get("/opds/recent", true)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(
				rec.Header().Get("Content-Type"),
			).To(ContainSubstring("kind=acquisition"))
			body := rec.Body.String()
			Expect(body).NotTo(ContainSubstring("Warbreaker"))
			Expect(body).To(ContainSubstring("Elantris"))
			Expect(
				strings.Index(body, "Mistborn"),
			).To(BeNumerically("<", strings.Index(body, "Elantris")))
		},
	)

	g.It(
		"picks the best format when a book has several ebook files",
		func(ctx g.SpecContext) {
			f.client.MediaFile.Create().
				SetPath("/x/elantris.pdf").
				SetSize(1).
				SetQuality("PDF").
				SetBookKind(mediafile.BookKindEbook).
				SetBook(f.ebook).
				SaveX(ctx)
			body := f.get("/opds/recent", true).Body.String()
			Expect(
				body,
			).To(ContainSubstring(fmt.Sprintf(`href="/opds/download/%d/epub"`, f.ebook.ID)))
			Expect(body).NotTo(ContainSubstring("/pdf"))
		},
	)

	g.It(
		"credits every maker of a book and lists it under each author or writer",
		func(ctx g.SpecContext) {
			oda := f.client.Author.Create().
				SetHardcoverID(20).
				SetName("Eiichiro Oda").
				SaveX(ctx)
			colorist := f.client.Author.Create().
				SetHardcoverID(21).
				SetName("Colorist Person").
				SaveX(ctx)
			translator := f.client.Author.Create().
				SetHardcoverID(22).
				SetName("Translator Person").
				SaveX(ctx)
			series := f.client.BookSeries.Create().
				SetHardcoverID(5).
				SetTitle("One Piece").
				SaveX(ctx)
			vol := newBook(
				ctx,
				f.client,
				30,
				"One Piece 1",
				oda,
				bookcontribution.RoleWriter,
			)
			f.client.Book.UpdateOne(vol).
				SetSeries(series).
				SetSeriesPosition(1).
				ExecX(ctx)
			f.client.BookContribution.Create().
				SetBook(vol).
				SetAuthor(colorist).SetRole(bookcontribution.RoleColorist).SaveX(ctx)
			f.client.BookContribution.Create().
				SetBook(vol).
				SetAuthor(translator).
				SetRole(bookcontribution.RoleTranslator).SaveX(ctx)
			f.client.MediaFile.Create().
				SetPath("/x/op1.cbz").SetSize(1).SetQuality("CBZ").
				SetBookKind(mediafile.BookKindEbook).SetBook(vol).SaveX(ctx)

			body := f.get(
				fmt.Sprintf("/opds/authors/%d", oda.ID),
				true,
			).Body.String()
			Expect(body).To(ContainSubstring("One Piece 1"))
			Expect(body).To(ContainSubstring("<name>Eiichiro Oda</name>"))
			Expect(body).To(ContainSubstring("<name>Colorist Person</name>"))
			Expect(body).NotTo(ContainSubstring("Translator Person"))
			Expect(body).To(ContainSubstring(`type="application/vnd.comicbook+zip"`))

			authors := f.get("/opds/authors", true).Body.String()
			Expect(authors).To(ContainSubstring("Eiichiro Oda"))
			Expect(authors).NotTo(ContainSubstring("Colorist Person"))
			Expect(authors).NotTo(ContainSubstring("Translator Person"))
		},
	)

	g.It(
		"searches a contributing author's name, not only the display name",
		func(ctx g.SpecContext) {
			artist := f.client.Author.Create().
				SetHardcoverID(23).
				SetName("Quiet Illustrator").
				SaveX(ctx)
			f.client.BookContribution.Create().
				SetBook(f.ebook).
				SetAuthor(artist).SetRole(bookcontribution.RoleArtist).SaveX(ctx)

			body := f.get("/opds/search?q=illustrator", true).Body.String()
			Expect(body).To(ContainSubstring("Elantris"))
			Expect(body).NotTo(ContainSubstring("Warbreaker"))
		},
	)

	g.It("describes search through OpenSearch", func() {
		rec := f.get("/opds/search.xml", true)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(
			rec.Header().Get("Content-Type"),
		).To(ContainSubstring("application/opensearchdescription+xml"))
		Expect(
			rec.Body.String(),
		).To(ContainSubstring("/opds/search?q={searchTerms}"))
	})

	g.It("searches title or author case-insensitively", func() {
		byTitle := f.get("/opds/search?q=elantris", true)
		Expect(byTitle.Code).To(Equal(http.StatusOK))
		Expect(byTitle.Body.String()).To(ContainSubstring("Elantris"))

		byAuthor := f.get("/opds/search?q=SANDERSON", true)
		Expect(byAuthor.Body.String()).To(ContainSubstring("Elantris"))
		Expect(byAuthor.Body.String()).NotTo(ContainSubstring("Warbreaker"))

		Expect(
			f.get("/opds/search?q=nothing-here", true).Body.String(),
		).NotTo(ContainSubstring("Elantris"))
	})

	g.It("rejects missing or wrong credentials with a Basic challenge", func() {
		rec := f.get("/opds", false)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
		Expect(rec.Header().Get("WWW-Authenticate")).To(ContainSubstring("Basic"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("<feed"))

		req := httptest.NewRequest(http.MethodGet, "/opds", nil)
		req.SetBasicAuth(testEmail, "wrong")
		bad := httptest.NewRecorder()
		f.router.ServeHTTP(bad, req)
		Expect(bad.Code).To(Equal(http.StatusUnauthorized))
	})
})
