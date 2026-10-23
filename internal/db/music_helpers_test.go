package db

import (
	"context"
	"time"

	. "github.com/onsi/gomega"

	"github.com/onsi/ginkgo/v2"
)

// hydrateAlbum stores tracks on the album with the release-group mbid, as the
// hydration worker would after the add.
func hydrateAlbum(
	ctx context.Context,
	store *DB,
	mbid string,
	tracks ...TrackSeed,
) {
	ginkgo.GinkgoHelper()
	a, err := store.FindAlbumByMBID(ctx, mbid)
	Expect(err).NotTo(HaveOccurred())
	Expect(a).NotTo(BeNil())
	Expect(store.SetAlbumHydration(
		ctx, a.ID, HydrationParams{Tracks: tracks}, time.Now(),
	)).To(Succeed())
}
