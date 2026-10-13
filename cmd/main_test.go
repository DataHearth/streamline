package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("rootCommand", Label("unit", "cli"), func() {
	DescribeTable("rejects a word that is not a subcommand before booting",
		func(arg string) {
			// run's default data_dir is ./data, so a boot would leave it here.
			dir := GinkgoT().TempDir()
			wd, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())
			Expect(os.Chdir(dir)).To(Succeed())
			DeferCleanup(os.Chdir, wd)

			err = rootCommand().Run(context.Background(), []string{"streamline", arg})
			want := fmt.Sprintf("unknown command %q", arg)
			Expect(err).To(MatchError(ContainSubstring(want)))

			entries, err := os.ReadDir(dir)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty())
		},
		Entry("version", "version"),
		Entry("a misspelled subcommand", "confg"),
	)

	It("prints the version for --version", func() {
		var out bytes.Buffer
		cmd := rootCommand()
		cmd.Writer = &out

		err := cmd.Run(context.Background(), []string{"streamline", "--version"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.String()).To(ContainSubstring(versionString()))
	})
})
