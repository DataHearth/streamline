package definitions_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"

	"github.com/dlclark/regexp2/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
)

// The snapshot is generated, so these specs guard the generator's output as
// committed: that it is current, that the model round-trips every byte of
// it, and that every regex runs on the engine it was assigned.
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

	It(
		"round-trips every definition byte for byte, and its regexes compile",
		func() {
			seen := map[string]bool{}
			for _, s := range cat.Definitions {
				Expect(seen[s.ID]).To(BeFalse(), "duplicate id %s", s.ID)
				seen[s.ID] = true

				d, err := definitions.Load(s.ID)
				Expect(err).NotTo(HaveOccurred(), s.ID)
				Expect(cardigann.Summarize(d)).To(Equal(s), s.ID)

				b, err := cardigann.EncodeJSON(d)
				Expect(err).NotTo(HaveOccurred())
				raw, err := os.ReadFile(filepath.Join("data", s.ID+".json"))
				Expect(err).NotTo(HaveOccurred())
				Expect(
					string(b),
				).To(Equal(string(raw)), "%s does not round-trip", s.ID)

				for _, f := range filters(reflect.ValueOf(d)) {
					if f.Name != "regexp" && f.Name != "re_replace" {
						continue
					}
					if f.Engine == cardigann.RegexEngineNET {
						_, err = regexp2.Compile(f.Args[0], regexp2.None)
					} else {
						_, err = regexp.Compile(f.Args[0])
					}
					Expect(err).NotTo(HaveOccurred(), "%s: %q", s.ID, f.Args[0])
				}
			}
		},
	)

	It("refuses an id that is not a plain name", func() {
		for _, id := range []string{"", "../index", "a/b", `a\b`, "nope"} {
			_, err := definitions.Load(id)
			Expect(err).To(MatchError(definitions.ErrNotFound), id)
		}
	})
})

func filters(v reflect.Value) []cardigann.Filter {
	var out []cardigann.Filter
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			out = append(out, filters(v.Elem())...)
		}
	case reflect.Struct:
		if f, ok := reflect.TypeAssert[cardigann.Filter](v); ok {
			return []cardigann.Filter{f}
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				out = append(out, filters(v.Field(i))...)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			out = append(out, filters(v.Index(i))...)
		}
	default:
	}
	return out
}
