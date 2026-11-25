package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("Metadata change store", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		owner  ChangeOwner
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)

		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title: "Dune", OriginalTitle: "Dune", Year: 2021, TmdbID: 438631,
			Status: entmovie.StatusWanted, QualityProfile: "HD",
		})
		Expect(err).NotTo(HaveOccurred())
		owner = ChangeOwner{ChangeOwnerMovie, m.ID}
	})

	report := func(fingerprint string) {
		GinkgoHelper()
		Expect(store.ReportChange(ctx, owner, metadatachange.KindMovieRemoved,
			fingerprint, map[string]any{"fp": fingerprint})).To(Succeed())
	}

	pending := func() []*ent.MetadataChange {
		GinkgoHelper()
		rows, total, err := store.ListChanges(ctx, ChangeFilter{}, 100, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(BeNumerically("==", len(rows)))
		return rows
	}

	It("keeps one row per owner and kind, carrying the latest payload", func() {
		report("a")
		report("b")
		rows := pending()
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Fingerprint).To(Equal("b"))
		Expect(rows[0].Payload).To(HaveKeyWithValue("fp", "b"))
		Expect(ChangeOwnerOf(rows[0])).To(Equal(owner))
	})

	It("does not ask again about a kept change, and does about a new one", func() {
		report("a")
		Expect(store.KeepChange(ctx, pending()[0].ID)).To(Succeed())
		Expect(pending()).To(BeEmpty())

		report("a")
		Expect(pending()).To(BeEmpty())

		report("b")
		Expect(pending()).To(HaveLen(1))
	})

	It("forgets a kept change once the condition is gone", func() {
		report("a")
		Expect(store.KeepChange(ctx, pending()[0].ID)).To(Succeed())
		Expect(
			store.ClearChange(ctx, owner, metadatachange.KindMovieRemoved),
		).To(Succeed())

		report("a")
		Expect(pending()).To(HaveLen(1))
	})

	It("counts pending changes by owner type and kind", func() {
		report("a")
		counts, err := store.CountChanges(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(counts.Total).To(BeNumerically("==", 1))
		Expect(
			counts.ByType,
		).To(HaveKeyWithValue(ChangeOwnerMovie, BeNumerically("==", 1)))
		Expect(
			counts.ByKind,
		).To(HaveKeyWithValue(metadatachange.KindMovieRemoved, BeNumerically("==", 1)))
	})

	It("filters by owner type", func() {
		report("a")
		rows, _, err := store.ListChanges(
			ctx,
			ChangeFilter{Types: []ChangeOwnerType{ChangeOwnerAlbum}},
			100,
			0,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(BeEmpty())
	})

	It("refuses to keep a change that is not pending", func() {
		Expect(ent.IsNotFound(store.KeepChange(ctx, 999))).To(BeTrue())
	})

	It("goes with its owner", func() {
		report("a")
		Expect(store.DeleteMovie(ctx, owner.ID)).To(Succeed())
		Expect(pending()).To(BeEmpty())
	})
})
