package config

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/providers/structs"
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

// listElementTypes pairs every name-keyed config list with the struct one of
// its entries unmarshals into, keyed by the dotted path to the list.
var listElementTypes = map[string]reflect.Type{
	"auth.oidc":            reflect.TypeFor[OIDCConfig](),
	"indexers":             reflect.TypeFor[IndexerEntry](),
	"download_clients":     reflect.TypeFor[DownloadClientEntry](),
	"quality_profiles":     reflect.TypeFor[QualityProfileEntry](),
	"media_server.servers": reflect.TypeFor[MediaServerEntry](),
	"custom_formats":       reflect.TypeFor[CustomFormatEntry](),
}

// itemProperties resolves the JSON Schema "properties" map describing one
// element of the list at the dotted path, following a local $ref for the lists
// whose item schema lives under definitions.
func itemProperties(doc map[string]any, path string) map[string]any {
	GinkgoHelper()
	cur := doc
	for seg := range strings.SplitSeq(path, ".") {
		props, ok := cur["properties"].(map[string]any)
		Expect(ok).To(BeTrue(), "%s: no properties at %q", path, seg)
		cur, ok = props[seg].(map[string]any)
		Expect(ok).To(BeTrue(), "%s: schema declares no %q", path, seg)
	}
	items, ok := cur["items"].(map[string]any)
	Expect(ok).To(BeTrue(), "%s: schema declares no items", path)
	if ref, ok := items["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/definitions/")
		Expect(name).NotTo(Equal(ref), "%s: unsupported $ref %q", path, ref)
		defs, ok := doc["definitions"].(map[string]any)
		Expect(ok).To(BeTrue(), "%s: schema has no definitions", path)
		items, ok = defs[name].(map[string]any)
		Expect(ok).To(BeTrue(), "%s: no definition %q", path, name)
	}
	props, ok := items["properties"].(map[string]any)
	Expect(ok).To(BeTrue(), "%s: item schema declares no properties", path)
	return props
}

var _ = Describe("config.SchemaJSON", Label("unit", "config"), func() {
	// The dumped-defaults specs below can only reach keys defaults() seeds, and
	// every list defaults to empty — so nothing above this exercises a single
	// per-element key, and the item schemas are additionalProperties: false. A
	// field added to one of these structs without a schema entry would make
	// every config that sets it invalid, silently, until an operator hit it.
	It("declares every field of every config list element", func() {
		raw, err := os.ReadFile(schemaPath)
		Expect(err).NotTo(HaveOccurred())
		var doc map[string]any
		Expect(json.Unmarshal(raw, &doc)).To(Succeed())

		for path, typ := range listElementTypes {
			props := itemProperties(doc, path)
			for _, f := range reflect.VisibleFields(typ) {
				tag := f.Tag.Get("koanf")
				if tag == "" {
					continue
				}
				Expect(props).To(HaveKey(tag), "%s[].%s missing from %s",
					path, tag, schemaPath)
			}
		}
	})

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

	// auth.oidc defaults to an empty list, so nothing above reaches the
	// per-provider properties — a key missing from the schema would silently
	// break every config that sets it (the item schema is
	// additionalProperties: false).
	Describe("auth.oidc[] properties", func() {
		withProvider := func(extra map[string]any) any {
			GinkgoHelper()
			doc := dumpedDefaultsDoc()
			p := map[string]any{
				"name":          "kc",
				"issuer":        "https://kc.example.com",
				"client_id":     "streamline",
				"client_secret": "secret",
			}
			maps.Copy(p, extra)
			doc["auth"].(map[string]any)["oidc"] = []any{p}
			return any(doc)
		}

		It("accepts every email_linking value the loader validates", func() {
			for _, v := range []string{
				OIDCEmailLinkingDisabled,
				OIDCEmailLinkingNonAdmin,
				OIDCEmailLinkingAll,
			} {
				Expect(compiledSchema().
					Validate(withProvider(map[string]any{"email_linking": v}))).
					To(Succeed(), "email_linking: %s", v)
			}
		})

		// writeYAMLAtomic marshals the whole struct, zero values included, so a
		// provider that never named email_linking was rewritten as
		// `email_linking: ""` on any config.Update — a file this very schema
		// rejects. Round-trip the real write path rather than asserting on the
		// normaliser, so the regression is caught wherever it reappears.
		It("validates the file it writes back for a provider omitting the key",
			func() {
				raw := "data_dir: " + GinkgoT().TempDir() + `
auth:
  oidc:
    - name: kc
      issuer: https://kc.example.com
      client_id: streamline
      client_secret: secret
`
				Expect(LoadReader(strings.NewReader(raw))).To(Succeed())
				DeferCleanup(ResetForTest)

				k := koanf.New(".")
				Expect(k.Load(structs.Provider(*Get(), "koanf"), nil)).
					To(Succeed())
				out, err := k.Marshal(yaml.Parser())
				Expect(err).NotTo(HaveOccurred())

				Expect(string(out)).
					To(ContainSubstring("email_linking: " + OIDCEmailLinkingDisabled))

				back := koanf.New(".")
				Expect(back.Load(rawbytes.Provider(out), yaml.Parser())).
					To(Succeed())
				Expect(compiledSchema().Validate(jsonNormalize(back.Raw()))).
					To(Succeed())
			})

		It("rejects an unknown email_linking value", func() {
			Expect(compiledSchema().
				Validate(withProvider(map[string]any{"email_linking": "always"}))).
				To(HaveOccurred())
		})

		It("accepts allow_admin either way", func() {
			for _, v := range []bool{false, true} {
				Expect(compiledSchema().
					Validate(withProvider(map[string]any{"allow_admin": v}))).
					To(Succeed(), "allow_admin: %v", v)
			}
		})

		It("rejects a non-boolean allow_admin", func() {
			Expect(compiledSchema().
				Validate(withProvider(map[string]any{"allow_admin": "yes"}))).
				To(HaveOccurred())
		})

		// allow_admin is a bool, so the write-back emits it for every provider
		// whether or not the file named it — the schema has to know the key or
		// streamline writes a config its own published schema rejects, the
		// regression email_linking already hit once.
		It("validates the file it writes back for a provider omitting allow_admin",
			func() {
				raw := "data_dir: " + GinkgoT().TempDir() + `
auth:
  oidc:
    - name: kc
      issuer: https://kc.example.com
      client_id: streamline
      client_secret: secret
`
				Expect(LoadReader(strings.NewReader(raw))).To(Succeed())
				DeferCleanup(ResetForTest)

				k := koanf.New(".")
				Expect(k.Load(structs.Provider(*Get(), "koanf"), nil)).To(Succeed())
				out, err := k.Marshal(yaml.Parser())
				Expect(err).NotTo(HaveOccurred())
				Expect(string(out)).To(ContainSubstring("allow_admin: false"))

				back := koanf.New(".")
				Expect(back.Load(rawbytes.Provider(out), yaml.Parser())).
					To(Succeed())
				Expect(compiledSchema().Validate(jsonNormalize(back.Raw()))).
					To(Succeed())
			})
	})
})
