package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("Store.ListUpcomingBooks", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		author *ent.Author
		from   time.Time
		to     time.Time
		id     uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
		author = client.Author.Create().
			SetHardcoverID(1).
			SetName("Frank Herbert").
			SaveX(ctx)
		from = time.Now().UTC().Truncate(time.Minute)
		to = from.Add(7 * 24 * time.Hour)
		id = 0
	})

	seed := func(title string, ebook, audiobook bool, date *time.Time) {
		GinkgoHelper()
		id++
		c := client.Book.Create().
			SetHardcoverID(id).
			SetTitle(title).
			SetEbookMonitored(ebook).
			SetAudiobookMonitored(audiobook).
			SetAuthor(author)
		if date != nil {
			c = c.SetReleaseDate(*date)
		}
		c.SaveX(ctx)
	}

	titles := func(books []*ent.Book) []string {
		out := make([]string, 0, len(books))
		for _, b := range books {
			out = append(out, b.Title)
		}
		return out
	}

	It("returns books with a monitored slot in [from,to), oldest first", func() {
		d1 := from.Add(24 * time.Hour)
		d2 := from.Add(2 * 24 * time.Hour)
		d3 := from.Add(3 * 24 * time.Hour)
		before := from.Add(-time.Hour)
		seed("audio-only", false, true, &d3)
		seed("ebook-only", true, false, &d1)
		seed("both", true, true, &d2)
		seed("unmonitored", false, false, &d1)
		seed("past", true, true, &before)
		seed("at-to", true, true, &to)
		seed("undated", true, true, nil)

		got, err := store.ListUpcomingBooks(ctx, from, to)
		Expect(err).NotTo(HaveOccurred())
		Expect(titles(got)).To(Equal([]string{"ebook-only", "both", "audio-only"}))
		Expect(got[0].Edges.Author).NotTo(BeNil())
		Expect(got[0].Edges.Author.Name).To(Equal("Frank Herbert"))
	})
})
