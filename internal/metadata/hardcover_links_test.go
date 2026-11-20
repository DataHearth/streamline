package metadata

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Hardcover edition links", Label("unit", "metadata"), func() {
	var (
		hc  *Hardcover
		ctx context.Context
		srv *served
	)

	BeforeEach(func() {
		ctx = context.Background()
		hc = newTestHardcover("test-token")
		srv = &served{}
	})

	// The rows are shaped as the live API answered them: 1984's paperback
	// lists two Goodreads ids, an Open Library edition and work, and Audible
	// one mapping per region.
	const body = `{"data":{"editions":[
	  {"id": 30399846,
	   "goodreads":[{"id":11894537,"external_id":"38718251","verified":null},
	                {"id":8686883,"external_id":"34168383","verified":true}],
	   "openlibrary":[{"id":1,"external_id":"/works/OL1168083W","verified":true},
	                  {"id":6622655,"external_id":"/books/OL27302768M","verified":true}],
	   "wikidata":[],
	   "google":[{"id":8686882,"external_id":"o-Y7ngAACAAJ","verified":null}],
	   "audible":[]},
	  {"id": 32763102,
	   "goodreads":[{"id":5,"external_id":"Ast??rix-Ren?? Goscinny-9.78179E+12","verified":null}],
	   "openlibrary":[], "wikidata":[], "google":[],
	   "audible":[{"id":20,"external_id":"B0FXY6DVJS:us","verified":null},
	              {"id":21,"external_id":"B0FXY6FR00:fr","verified":null},
	              {"id":22,"external_id":"not-an-asin","verified":null}]}
	]}}`

	It("keeps one well-formed id per site, a verified one first", func() {
		hc.client.Transport = srv.transport(body)

		got, err := hc.EditionLinks(ctx, []uint32{30399846, 32763102, 999})

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[uint32]map[string]string{
			30399846: {
				SourceGoodreads:   "34168383",
				SourceOpenLibrary: "OL27302768M",
				SourceGoogleBooks: "o-Y7ngAACAAJ",
			},
			// A malformed Goodreads value is no link; Audible falls back to
			// the US store for an English library.
			32763102: {SourceAudible: "B0FXY6DVJS:us"},
		}))
		// One request for every edition, all platforms in it, the platform by
		// id (depth 2) and the unloaded mappings left out.
		Expect(srv.queries).To(HaveLen(1))
		Expect(srv.queries[0]).To(ContainSubstring(`platform_id: {_eq: 32}`))
		Expect(srv.queries[0]).To(ContainSubstring(`state: {_eq: "normalized"}`))
		Expect(srv.queries[0]).NotTo(ContainSubstring("platform {"))
	})

	It("opens the library language's Audible store when Hardcover lists it", func() {
		configtest.Setup(
			map[string]any{"library": map[string]any{"book_language": "fr"}},
		)
		hc.client.Transport = srv.transport(body)

		got, err := hc.EditionLinks(ctx, []uint32{32763102})

		Expect(err).NotTo(HaveOccurred())
		Expect(got[32763102][SourceAudible]).To(Equal("B0FXY6FR00:fr"))
	})

	It("reads a hundred editions per request", func() {
		hc.client.Transport = srv.transport(`{"data":{"editions":[]}}`)
		ids := make([]uint32, 0, 150)
		for i := range 150 {
			ids = append(ids, uint32(i+1))
		}

		_, err := hc.EditionLinks(ctx, ids)

		Expect(err).NotTo(HaveOccurred())
		Expect(srv.queries).To(HaveLen(2))
		Expect(fmt.Sprint(srv.vars[0]["ids"])).To(HavePrefix("[1 2 3"))
		Expect(strings.Count(fmt.Sprint(srv.vars[1]["ids"]), " ")).To(Equal(49))
	})
})
