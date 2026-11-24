package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The SPA translates a view's permission line by view name and shows the
// catalogue's English description for a name it has no message for, so a view
// added here without its message reads in English in every locale.
var _ = Describe("Extension view labels", Label("unit", "db"), func() {
	const app = "../../web/app"

	It("has a message in every locale for each view with a line of its own", func() {
		files, err := filepath.Glob(filepath.Join(app, "messages", "*.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(BeEmpty())

		locales := map[string]map[string]any{}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())
			var messages map[string]any
			Expect(json.Unmarshal(raw, &messages)).To(Succeed(), f)
			locales[filepath.Base(f)] = messages
		}
		Expect(locales).To(HaveKey("en.json"))

		labels, err := os.ReadFile(filepath.Join(app, "lib", "extensions.ts"))
		Expect(err).NotTo(HaveOccurred())

		for _, v := range ExtensionViews() {
			// A bundled view whose description is its bundle's reads as the
			// bundle in the SPA and needs no message.
			if v.Bundle != "" &&
				locales["en.json"]["extensions_bundle_"+v.Bundle] == v.Description {
				continue
			}
			key := "extensions_view_" + v.Name
			for name, messages := range locales {
				Expect(
					messages,
				).To(HaveKey(key), "%s has no message for the %s view", name, v.Name)
			}
			Expect(
				string(labels),
			).To(ContainSubstring(fmt.Sprintf("%s: i18n.%s,", v.Name, key)),
				"VIEW_LABEL in web/app/lib/extensions.ts has no entry for the %s view", v.Name)
		}
	})
})
