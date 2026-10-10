package book

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dlmocks "github.com/datahearth/streamline/internal/download/mocks"
	idxmocks "github.com/datahearth/streamline/internal/indexer/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	metamocks "github.com/datahearth/streamline/internal/metadata/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// fakePosters records what the service fetches and removes. A mock would fail
// the spec when the background fetch finishes after it.
type fakePosters struct {
	mu      sync.Mutex
	fetched []fetch
	removed []fetch
}

type fetch struct {
	kind string
	id   uint32
	src  string
}

func (p *fakePosters) Fetch(
	_ context.Context,
	kind string,
	id uint32,
	src string,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetched = append(p.fetched, fetch{kind, id, src})
	return nil
}

func (p *fakePosters) Put(
	context.Context,
	string,
	uint32,
	io.Reader,
) error {
	return nil
}

func (p *fakePosters) Serve(http.ResponseWriter, *http.Request, string, uint32) {}

func (p *fakePosters) Path(
	kind string,
	id uint32,
) string {
	return "/posters/" + kind
}

func (p *fakePosters) Remove(kind string, id uint32) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removed = append(p.removed, fetch{kind: kind, id: id})
	return nil
}

func (p *fakePosters) fetches() []fetch {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]fetch(nil), p.fetched...)
}

func (p *fakePosters) removals() []fetch {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]fetch(nil), p.removed...)
}

// budgetedProvider is the mock provider with a daily budget the spec controls.
type budgetedProvider struct {
	*metamocks.MockBookProvider
	remaining, reserve int
}

func (b *budgetedProvider) Remaining() int   { return b.remaining }
func (b *budgetedProvider) ScanReserve() int { return b.reserve }

type fixture struct {
	ctx     context.Context
	client  *ent.Client
	store   *db.DB
	meta    *metamocks.MockBookProvider
	posters *fakePosters
	idx     *idxmocks.MockManager
	dl      *dlmocks.MockDownloader
	svc     *Service
	dir     string
}

func bookConfig() map[string]any {
	return map[string]any{
		"book_quality_profiles": []map[string]any{{
			"name":            "std",
			"upgrade_allowed": true,
			"ebook": map[string]any{
				"formats": []string{"EPUB", "CBZ"}, "preferred": "EPUB",
			},
			"audiobook": map[string]any{
				"formats": []string{"M4B", "MP3"}, "preferred": "M4B",
				"min_bitrate": 64,
			},
		}, {
			"name": "comics",
			"ebook": map[string]any{
				"formats": []string{"CBZ"}, "preferred": "CBZ",
			},
			"audiobook": map[string]any{
				"formats": []string{"M4B"}, "preferred": "M4B",
			},
		}},
		"book_quality_default_profiles": map[string]any{
			"novel": "std", "bd": "comics", "comic": "comics", "manga": "comics",
		},
	}
}

func newFixture() *fixture {
	GinkgoHelper()
	f := &fixture{ctx: context.Background(), dir: GinkgoT().TempDir()}
	cfg := bookConfig()
	cfg["library"] = map[string]any{
		"ebook_path":        f.dir + "/ebooks",
		"audiobook_path":    f.dir + "/audio",
		"no_match_cooldown": "6h",
		"max_grab_failures": 3,
		"book_language":     "en",
	}
	cfg["download_clients"] = []map[string]any{{
		"name":        "qbit",
		"client_type": "qbittorrent",
		"host":        "127.0.0.1",
		"port":        8080,
		"auth_method": "password",
		"enabled":     true,
	}}
	configtest.Setup(cfg)

	var err error
	f.client, err = db.Open(f.ctx, ":memory:")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { f.client.Close() })
	f.store = db.New(f.client)
	f.meta = metamocks.NewMockBookProvider(GinkgoT())
	f.posters = &fakePosters{}
	f.idx = idxmocks.NewMockManager(GinkgoT())
	f.dl = dlmocks.NewMockDownloader(GinkgoT())
	f.svc = NewService(f.store, f.meta, f.posters, f.idx, f.dl, nil)
	return f
}

// rec builds a Hardcover record with an English and a French ebook and an
// English audiobook, each in its own publisher.
func rec(id uint32, title string) *metadata.BookRecord {
	return &metadata.BookRecord{
		HardcoverID: id,
		Title:       title,
		Description: "About " + title,
		ReleaseYear: 2005,
		Kind:        metadata.BookKindNovel,
		CoverURL:    "https://img/b" + itoa(id) + ".jpg",
		Credits: []metadata.BookCredit{{
			AuthorHardcoverID: 10, Name: "Brandon Sanderson",
			ImageURL: "https://img/a10.jpg", Role: metadata.RoleAuthor,
		}},
		OriginalLanguage: "en",
		Editions: []metadata.EditionRecord{
			{
				HardcoverID: id*10 + 1,
				Language:    "en",
				Title:       title,
				Publisher:   "Tor",
				Year:        2005,
				Format:      metadata.FormatEbook,
				Original:    true,
				Popularity:  9,
			},
			{
				HardcoverID: id*10 + 2,
				Language:    "fr",
				Title:       title + " (fr)",
				Publisher:   "Mnemos",
				Year:        2008,
				Format:      metadata.FormatEbook,
				Popularity:  4,
			},
			{
				HardcoverID: id*10 + 3,
				Language:    "en",
				Title:       title,
				Publisher:   "Audible",
				Year:        2006,
				Format:      metadata.FormatAudiobook,
				Original:    true,
				Popularity:  3,
			},
		},
	}
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

// addBook adds a book through the service the way the API does.
func (f *fixture) addBook(hc uint32, title string, monitor string) *ent.Book {
	GinkgoHelper()
	f.meta.EXPECT().GetBooks(anyCtx, ids(hc)).
		Return([]*metadata.BookRecord{rec(hc, title)}, nil).Once()
	b, err := f.svc.AddBook(f.ctx, AddBookParams{HardcoverID: hc, Monitor: monitor})
	Expect(err).NotTo(HaveOccurred())
	return b
}

func (f *fixture) reloadBook(id uint32) *ent.Book {
	GinkgoHelper()
	b, err := f.store.FindBookByID(f.ctx, id)
	Expect(err).NotTo(HaveOccurred())
	return b
}

func (f *fixture) reloadSeries(id uint32) *ent.BookSeries {
	GinkgoHelper()
	s, err := f.store.FindSeriesByID(f.ctx, id)
	Expect(err).NotTo(HaveOccurred())
	return s
}
