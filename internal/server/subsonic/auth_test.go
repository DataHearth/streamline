package subsonic

import (
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"net/url"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = g.Describe("auth", g.Label("unit"), func() {
	g.It("accepts a valid token: t = md5(password + salt)", func() {
		sum := md5.Sum([]byte("sesame" + "abc123"))
		token := hex.EncodeToString(sum[:])
		Expect(
			checkCredentials("sesame", url.Values{"t": {token}, "s": {"abc123"}}),
		).To(BeTrue())
	})

	g.It("accepts legacy p= plain and enc: hex forms", func() {
		Expect(checkCredentials("sesame", url.Values{"p": {"sesame"}})).To(BeTrue())
		Expect(
			checkCredentials(
				"sesame",
				url.Values{"p": {"enc:" + hex.EncodeToString([]byte("sesame"))}},
			),
		).To(BeTrue())
	})

	g.It("rejects wrong token and empty stored password", func() {
		Expect(
			checkCredentials("sesame", url.Values{"t": {"beef"}, "s": {"x"}}),
		).To(BeFalse())
		Expect(checkCredentials("", url.Values{"p": {""}})).To(BeFalse())
	})
})
