package api

import (
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("REST API body limit", Label("e2e"), func() {
	// Only the authenticated surface is exercised here. An oversized
	// /auth/login would spend one of the suite's 5 per-IP login attempts, and
	// the server integration suite already covers that route.
	It("refuses an oversized JSON body with 413", func() {
		body := []byte(
			`{"name":"` + strings.Repeat("a", (1<<20)+4096) +
				`","preferred_resolution":"1080p"}`,
		)

		resp := postRaw("/api/v1/quality-profiles", adminAuth, body)
		defer resp.Body.Close()

		Expect(resp.StatusCode).To(
			Equal(http.StatusRequestEntityTooLarge),
			"body limit not enforced: %s", bodyText(resp),
		)
	})
})
