package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The SPA builds every in-app address from the ROUTE table in
// app/lib/routes.ts. A pattern there that no route file serves is a link to
// Routify's 404, which nothing else would notice until someone clicked it.
var _ = Describe("SPA route table", Label("unit", "web"), func() {
	It("names only patterns a route file serves", func() {
		src, err := os.ReadFile(filepath.Join("app", "lib", "routes.ts"))
		Expect(err).NotTo(HaveOccurred())

		table := regexp.MustCompile(`(?s)export const ROUTE = \{(.*?)\} as const;`).
			FindSubmatch(src)
		Expect(table).NotTo(BeNil(), "ROUTE table not found in routes.ts")
		entries := regexp.MustCompile(`(?m)^\s*(\w+): "([^"]+)",`).
			FindAllSubmatch(table[1], -1)
		Expect(entries).NotTo(BeEmpty())

		for _, e := range entries {
			name, pattern := string(e[1]), string(e[2])
			Expect(routeFile(pattern)).To(
				BeAnExistingFile(),
				"ROUTE.%s (%s) has no route file", name, pattern,
			)
		}
	})
})

// routeFile is the Routify file serving a pattern: /movies/[id] is
// routes/movies/[id].svelte, /movies is routes/movies/index.svelte (or
// routes/movies.svelte), / is routes/index.svelte.
func routeFile(pattern string) string {
	rel := strings.Trim(pattern, "/")
	base := filepath.Join("app", "routes", filepath.FromSlash(rel))
	if rel == "" {
		return filepath.Join(base, "index.svelte")
	}
	if strings.HasSuffix(rel, "]") {
		return base + ".svelte"
	}
	//nolint:gosec // G703: base is a pattern of the repo's own routes.ts, stat'ed only to test it
	if _, err := os.Stat(filepath.Join(base, "index.svelte")); err == nil {
		return filepath.Join(base, "index.svelte")
	}
	return base + ".svelte"
}
