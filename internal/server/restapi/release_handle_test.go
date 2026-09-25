package restapi

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("release handles", Label("unit", "server"), func() {
	const link = "http://10.0.0.4:9117/dl/tracker/?jackett_apikey=SECRETKEY&path=abc"

	withSecret := func(secret string) {
		GinkgoHelper()
		configtest.Setup(map[string]any{
			"auth": map[string]any{"session_secret": secret},
		})
	}

	BeforeEach(func() { withSecret("first-secret") })

	It("hides the link and opens back to it", func() {
		handle := sealReleaseLink(link)

		Expect(handle).To(HavePrefix(releaseHandlePrefix))
		Expect(handle).NotTo(ContainSubstring("SECRETKEY"))
		Expect(handle).NotTo(ContainSubstring("10.0.0.4"))
		Expect(openReleaseLink(handle)).To(Equal(link))
	})

	It("passes a plain link through untouched", func() {
		Expect(openReleaseLink("magnet:?xt=urn:btih:abc")).
			To(Equal("magnet:?xt=urn:btih:abc"))
	})

	It("refuses a tampered handle", func() {
		handle := []byte(sealReleaseLink(link))
		mid := len(handle) / 2
		if handle[mid] == 'A' {
			handle[mid] = 'B'
		} else {
			handle[mid] = 'A'
		}

		_, err := openReleaseLink(string(handle))

		Expect(err).To(MatchError(errBadReleaseHandle))
	})

	It("retires every handle when the session secret rotates", func() {
		handle := sealReleaseLink(link)
		withSecret("rotated-secret")

		_, err := openReleaseLink(handle)

		Expect(err).To(MatchError(errBadReleaseHandle))
	})

	It("keeps the indexer's key out of a member's search result", func() {
		out := toSearchResult(indexer.SearchResult{
			Title:    "Flick.2024.1080p.WEB-GRP",
			Download: link,
			InfoURL:  "http://10.0.0.4:9117/details/1?jackett_apikey=SECRETKEY",
		})

		Expect(out.DownloadUrl).NotTo(ContainSubstring("SECRETKEY"))
		Expect(*out.InfoUrl).NotTo(ContainSubstring("SECRETKEY"))
		Expect(strings.HasPrefix(*out.InfoUrl, "http://10.0.0.4:9117/details/1")).
			To(BeTrue())

		back, err := toIndexerResult(&out)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Download).To(Equal(link))
	})
})
