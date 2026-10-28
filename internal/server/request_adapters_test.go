package server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("findAlbum", Label("unit", "server"), func() {
	It("finds an album the artist only co-credits", func() {
		shared := &ent.Album{ID: 7, Mbid: "rg-1"}
		a := &ent.Artist{Edges: ent.ArtistEdges{
			Albums:   []*ent.Album{{ID: 1, Mbid: "rg-own"}},
			CoAlbums: []*ent.Album{shared},
		}}
		Expect(findAlbum(a, "rg-1")).To(BeIdenticalTo(shared))
	})
})
