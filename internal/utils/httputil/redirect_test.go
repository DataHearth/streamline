package httputil

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SafeNextPath", Label("unit"), func() {
	DescribeTable("keeps an in-app path",
		func(in, want string) {
			got, ok := SafeNextPath(in)

			Expect(ok).To(BeTrue())
			Expect(got).To(Equal(want))
		},
		Entry("a bare path", "/movies", "/movies"),
		Entry("a path with a query", "/movies?page=2", "/movies?page=2"),
		Entry("the root", "/", "/"),
		Entry("re-emits an encoded path encoded", "/a%20b", "/a%20b"),
		Entry("a path that merely starts with auth", "/authors", "/authors"),
	)

	// Browsers fold "\" into "/" inside a Location value, so every one of these
	// leaves the site despite passing a rooted-path check by eye.
	DescribeTable("refuses a value that can leave the site",
		func(in string) {
			got, ok := SafeNextPath(in)

			Expect(ok).To(BeFalse())
			Expect(got).To(BeEmpty())
		},
		Entry("empty", ""),
		Entry("protocol-relative", "//evil.example"),
		Entry("backslash after the root", `/\evil.example`),
		Entry("double backslash", `/\\evil.example`),
		Entry("encoded backslash", "/%5Cevil.example"),
		Entry("backslash after a valid path", `/movies\evil.example`),
		Entry("encoded double slash", "/%2f%2fevil.example"),
		Entry("absolute url", "https://evil.example"),
		Entry("scheme-relative with userinfo", "//user@evil.example"),
		Entry("a tab browsers strip", "/\t/evil.example"),
		Entry("not rooted", "movies"),
		Entry("an opaque url", "mailto:a@b.c"),
		Entry("bounces back into the SSO flow", "/auth/oidc/keycloak/start"),
	)
})
