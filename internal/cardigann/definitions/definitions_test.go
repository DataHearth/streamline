package definitions_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
)

// The snapshot is generated, so these specs guard the generator's output as
// committed: that it is current, that the model round-trips every byte of
// it, and that convert.Check accepts every file — each regex, in a filter or
// a template, compiles on the engine it was assigned, and each template
// parses. They cannot tell whether the converter changed since the sync ran;
// that is what bumping convert.Version is for.
var _ = Describe("embedded snapshot", Label("unit", "cardigann"), func() {
	var cat cardigann.Catalog

	BeforeEach(func() {
		var err error
		cat, err = definitions.Catalog()
		Expect(err).NotTo(HaveOccurred())
	})

	It("was written by this converter for this schema", func() {
		Expect(cat.Converter).To(Equal(convert.Version),
			"convert.Version moved without a re-sync: run task cardigann:sync")
		Expect(cat.Schema).To(Equal(cardigann.SchemaVersion))
		Expect(cat.Upstream.Commit).To(MatchRegexp(`^[0-9a-f]{40}$`))
		Expect(cat.Definitions).NotTo(BeEmpty())
	})

	It("carries the attribution notice for the revision it embeds", func() {
		Expect(definitions.Notice()).To(ContainSubstring(cat.Upstream.Commit))
		Expect(definitions.Notice()).To(ContainSubstring("Jackett"))
	})

	It("round-trips every definition byte for byte, and Check accepts it", func() {
		for _, s := range cat.Definitions {
			d, err := definitions.Load(s.File)
			Expect(err).NotTo(HaveOccurred(), s.File)
			Expect(cardigann.Summarize(s.File, d)).To(Equal(s), s.File)
			Expect(d.Source.Path).To(HaveSuffix("/"+s.File+".yml"), s.File)

			b, err := cardigann.EncodeJSON(d)
			Expect(err).NotTo(HaveOccurred())
			raw, err := os.ReadFile(filepath.Join("data", s.File+".json"))
			Expect(err).NotTo(HaveOccurred())
			Expect(
				string(b),
			).To(Equal(string(raw)), "%s does not round-trip", s.File)

			Expect(convert.Check(d)).To(Succeed(), s.File)
		}
	})

	It("loads by upstream file name, which is not always the id", func() {
		d, err := definitions.Load("bluebird")
		Expect(err).NotTo(HaveOccurred())
		Expect(d.ID).To(Equal("bluebirdhd"))
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

	It("refuses a name that is not a plain file name", func() {
		for _, id := range []string{"", "../index", "a/b", `a\b`, "nope"} {
			_, err := definitions.Load(id)
			Expect(err).To(MatchError(definitions.ErrNotFound), id)
		}
	})
})
