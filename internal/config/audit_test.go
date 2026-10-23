package config

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("changedKeys", Label("unit", "config"), func() {
	It("redacts a secret held inside a list entry", func() {
		prev := &Config{}
		prev.Indexers = []IndexerEntry{{Name: "tracker", APIKey: "old-key"}}
		next := &Config{}
		next.Indexers = []IndexerEntry{{Name: "tracker", APIKey: "new-key"}}
		p, err := flatten(prev)
		Expect(err).NotTo(HaveOccurred())
		n, err := flatten(next)
		Expect(err).NotTo(HaveOccurred())

		changed := strings.Join(changedKeys(p.All(), n.All()), ", ")

		Expect(changed).To(ContainSubstring("indexers.0.api_key: changed"))
		Expect(changed).NotTo(ContainSubstring("old-key"))
		Expect(changed).NotTo(ContainSubstring("new-key"))
	})

	It("still names a non-secret field that moved inside a list entry", func() {
		p, err := flatten(&Config{Indexers: []IndexerEntry{{Name: "a"}}})
		Expect(err).NotTo(HaveOccurred())
		n, err := flatten(&Config{Indexers: []IndexerEntry{{Name: "b"}}})
		Expect(err).NotTo(HaveOccurred())

		Expect(changedKeys(p.All(), n.All())).
			To(ContainElement("indexers.0.name: a → b"))
	})
})
