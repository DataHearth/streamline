package definitions_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/testutil/cardiganntest"
)

var _ = Describe("the definition catalog", Label("unit", "cardigann"), func() {
	It("says nothing has been fetched before the first fetch", func() {
		prev := definitions.Install(nil)
		DeferCleanup(func() { definitions.Install(prev) })
		Expect(
			definitions.Catalog(),
		).Error().
			To(MatchError(definitions.ErrNotFetched))
		Expect(
			definitions.Raw("yts"),
		).Error().
			To(MatchError(definitions.ErrNotFetched))
	})

	When("a fetch is in force", func() {
		var cat cardigann.Catalog

		BeforeEach(func() {
			cardiganntest.Install()
			var err error
			cat, err = definitions.Catalog()
			Expect(err).NotTo(HaveOccurred())
		})

		It("decodes every definition it lists, which Check accepts", func() {
			Expect(cat.Definitions).NotTo(BeEmpty())
			Expect(cat.Converter).To(Equal(convert.Version))
			Expect(cat.Upstream).To(Equal(cardiganntest.Upstream))
			for _, s := range cat.Definitions {
				raw, err := definitions.Raw(s.File)
				Expect(err).NotTo(HaveOccurred(), s.File)
				d, err := definitions.Decode(raw)
				Expect(err).NotTo(HaveOccurred(), s.File)
				Expect(cardigann.Summarize(s.File, d)).To(Equal(s), s.File)
				Expect(d.Source.Path).To(HaveSuffix("/"+s.File+".yml"), s.File)
				Expect(convert.Check(d)).To(Succeed(), s.File)
			}
		})

		It("serves a version pinned to its commit without keeping it", func() {
			dir := GinkgoT().TempDir()
			b, err := definitions.Pinned(dir, "yts", cardiganntest.Upstream.Commit)
			Expect(err).NotTo(HaveOccurred())
			Expect(b).To(Equal(cardiganntest.Spec("yts")))
			Expect(filepath.Join(dir, "cardigann")).NotTo(BeADirectory())
			Expect(definitions.Decode([]byte("{"))).Error().To(HaveOccurred())
		})

		It("hands each caller its own copy of the catalog", func() {
			cat.Definitions[0].Categories = append(
				cat.Definitions[0].Categories[:0],
				"x",
			)
			cat.Definitions = cat.Definitions[:1]
			again, err := definitions.Catalog()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(again.Definitions)).To(BeNumerically(">", 1))
			Expect(again.Definitions[0].Categories).NotTo(ContainElement("x"))
		})

		It("knows no definition it was not given", func() {
			for _, id := range []string{"", "../index", "nope"} {
				Expect(definitions.Raw(id)).Error().
					To(MatchError(definitions.ErrNotFound), id)
			}
		})
	})

	Describe("the versions indexers are pinned to", func() {
		var dir string

		BeforeEach(func() { dir = GinkgoT().TempDir() })

		It("keeps one under the data directory until it is pruned", func() {
			spec := cardiganntest.Spec("yts")
			commit := cardiganntest.Commit(spec)
			Expect(definitions.Pinned(dir, "yts", commit)).Error().
				To(MatchError(definitions.ErrNotKept))

			Expect(cardiganntest.Keep(dir, "yts", spec)).To(Equal(commit))
			Expect(definitions.Pinned(dir, "yts", commit)).To(Equal(spec))

			other := cardiganntest.Keep(dir, "13city", cardiganntest.Spec("13city"))
			stale := filepath.Join(dir, "cardigann", "v0", commit)
			Expect(os.MkdirAll(stale, 0o750)).To(Succeed())
			Expect(definitions.Prune(dir, map[string]bool{other: true})).
				To(Succeed())
			Expect(definitions.Pinned(dir, "yts", commit)).Error().
				To(MatchError(definitions.ErrNotKept))
			Expect(definitions.Pinned(dir, "13city", other)).NotTo(BeEmpty())
			Expect(stale).NotTo(BeADirectory())
		})

		It("refuses a commit or a file name that is not upstream's", func() {
			commit := cardiganntest.Upstream.Commit
			Expect(definitions.Keep(dir, "../yts", commit, []byte("{}"))).
				To(MatchError(definitions.ErrNotFound))
			Expect(definitions.Keep(dir, "yts", "../x", []byte("{}"))).
				NotTo(Succeed())
			Expect(definitions.Pinned(dir, "yts", "")).Error().
				To(MatchError(ContainSubstring("cardigann-update job")))
		})
	})
})
