package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil"
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

// The server refuses to boot on a session secret it cannot sign with, and
// `config validate` is where an operator expects to hear that first.
var _ = Describe("config validate", Label("unit", "cli"), func() {
	validate := func(secretFile string) error {
		GinkgoHelper()
		DeferCleanup(testutil.IsolateEnv())
		DeferCleanup(config.ResetForTest)
		dir := GinkgoT().TempDir()
		secret := filepath.Join(dir, "session_secret")
		Expect(os.WriteFile(secret, []byte(secretFile), 0o600)).To(Succeed())
		path := filepath.Join(dir, "config.yaml")
		raw := fmt.Sprintf(
			"data_dir: %q\nauth:\n  session_secret_file: %q\n",
			dir, secret,
		)
		Expect(os.WriteFile(path, []byte(raw), 0o600)).To(Succeed())
		return rootCommand().Run(
			context.Background(),
			[]string{"streamline", "config", "validate", "--config", path},
		)
	}

	It("reports a session_secret_file that reads back empty", func() {
		err := validate(" \n")
		Expect(err).To(MatchError(ContainSubstring("auth.session_secret_file")))
		Expect(err).To(MatchError(ContainSubstring("is empty")))
	})

	It("accepts a session_secret_file that holds a secret", func() {
		Expect(validate("a-real-session-secret\n")).To(Succeed())
	})
})
