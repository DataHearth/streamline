package extension

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("settings cards", Label("unit"), func() {
	manifestWith := func(fields string) []byte {
		return fmt.Appendf(nil, `{"name":"x","version":"1","streamline_api":1,
			"account_cards":[{"id":"s","kind":"settings","title":"S","fields":%s}]}`, fields)
	}

	DescribeTable(
		"the manifest",
		func(fields string, ok bool) {
			_, err := parseManifest(manifestWith(fields))
			if ok {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ErrBadManifest))
			}
		},
		Entry("a switch and a number", `[
			{"id":"allow","kind":"switch","label":"Allow"},
			{"id":"limit","kind":"number","label":"Limit","min":0,"max":320}]`, true),
		Entry("no fields", `[]`, false),
		Entry("an empty id", `[{"id":"","kind":"switch","label":"A"}]`, false),
		Entry("a repeated id", `[
			{"id":"a","kind":"switch","label":"A"},
			{"id":"a","kind":"switch","label":"B"}]`, false),
		Entry("an unknown kind", `[{"id":"a","kind":"slider","label":"A"}]`, false),
		Entry(
			"min above max",
			`[{"id":"a","kind":"number","label":"A","min":5,"max":1}]`,
			false,
		),
	)

	card := AccountCard{
		ID: "s", Kind: CardSettings,
		Fields: []CardField{
			{ID: "allow", Kind: FieldSwitch, Label: "Allow"},
			{ID: "limit", Kind: FieldNumber, Label: "Limit", Min: 0, Max: 320},
		},
	}

	DescribeTable("a write",
		func(body string, ok bool) {
			err := card.CheckValues([]byte(body))
			if ok {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ErrBadCardValues))
			}
		},
		Entry("both fields", `{"values":{"allow":true,"limit":128}}`, true),
		Entry("one field", `{"values":{"limit":320}}`, true),
		Entry("the lower bound", `{"values":{"limit":0}}`, true),
		Entry("above the bound", `{"values":{"limit":321}}`, false),
		Entry("below the bound", `{"values":{"limit":-1}}`, false),
		Entry("an undeclared key", `{"values":{"other":1}}`, false),
		Entry("a number for a switch", `{"values":{"allow":1}}`, false),
		Entry("null for a switch", `{"values":{"allow":null}}`, false),
		Entry("null for a number", `{"values":{"limit":null}}`, false),
		Entry("a string for a number", `{"values":{"limit":"9"}}`, false),
		Entry("a fraction", `{"values":{"limit":1.5}}`, false),
		Entry("no values", `{}`, false),
		Entry("not JSON", `nope`, false),
	)
})
