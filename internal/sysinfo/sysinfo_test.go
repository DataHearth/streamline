package sysinfo

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("displayVersion", Label("unit", "sysinfo"), func() {
	DescribeTable("renders one canonical version string",
		func(injected, want string) {
			Expect(displayVersion(injected)).To(Equal(want))
		},
		Entry("goreleaser injects a bare semver", "1.3.0", "v1.3.0"),
		Entry("the image build injects the tag", "v1.3.0", "v1.3.0"),
		Entry("a plain go build injects nothing", "", "dev"),
		Entry("a branch build injects the branch name", "main", "main"),
		Entry("a branch whose name starts with v", "v2-rewrite", "v2-rewrite"),
		Entry("a sha-tagged build", "sha-ae1acfa", "sha-ae1acfa"),
	)
})
