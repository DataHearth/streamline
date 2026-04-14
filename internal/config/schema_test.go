package config

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// jsonNormalize round-trips through encoding/json so the value uses the
// canonical types (map[string]any, []any, float64, …) the validator expects.
func jsonNormalize(v any) any {
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	var out any
	Expect(json.Unmarshal(b, &out)).To(Succeed())
	return out
}

// schemaPath is the standalone config JSON Schema, shipped alongside the
// OpenAPI spec in api/ so editors / CI can point at it directly.
const schemaPath = "../../api/config.schema.json"

func compiledSchema() *jsonschema.Schema {
	GinkgoHelper()
	raw, err := os.ReadFile(schemaPath)
	Expect(err).NotTo(HaveOccurred())
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	Expect(err).NotTo(HaveOccurred())
	c := jsonschema.NewCompiler()
	Expect(c.AddResource("config.schema.json", doc)).To(Succeed())
	sch, err := c.Compile("config.schema.json")
	Expect(err).NotTo(HaveOccurred())
	return sch
}

func dumpedDefaultsDoc() map[string]any {
	GinkgoHelper()
	var buf bytes.Buffer
	Expect(DumpDefaults(&buf)).To(Succeed())
	k := koanf.New(".")
	Expect(
		k.Load(rawbytes.Provider(buf.Bytes()), yaml.Parser()),
	).To(Succeed())
	return jsonNormalize(k.Raw()).(map[string]any)
}

var _ = Describe("config.SchemaJSON", Label("unit", "config"), func() {
	It("accepts the canonical default config", func() {
		Expect(compiledSchema().Validate(any(dumpedDefaultsDoc()))).
			To(Succeed())
	})

	It("rejects an unknown top-level key", func() {
		doc := dumpedDefaultsDoc()
		doc["bogus_key"] = "nope"
		Expect(compiledSchema().Validate(any(doc))).To(HaveOccurred())
	})

	It("rejects an invalid enum value", func() {
		doc := dumpedDefaultsDoc()
		doc["auth"].(map[string]any)["mode"] = "not-a-mode"
		Expect(compiledSchema().Validate(any(doc))).To(HaveOccurred())
	})
})
