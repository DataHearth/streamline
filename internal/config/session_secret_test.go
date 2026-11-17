package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("session secret", Label("unit", "config"), func() {
	BeforeEach(func() {
		ResetForTest()
		DeferCleanup(ResetForTest)
	})

	atFloor := strings.Repeat("k", MinSessionSecretLen)

	// load reads a config naming the secret inline, or through a file
	// holding it.
	load := func(secret string, byFile bool) error {
		GinkgoHelper()
		dir := GinkgoT().TempDir()
		raw := fmt.Sprintf("data_dir: %q\nauth:\n", dir)
		if byFile {
			path := filepath.Join(dir, "session_secret")
			Expect(os.WriteFile(path, []byte(secret), 0o600)).To(Succeed())
			raw += fmt.Sprintf("  session_secret_file: %q\n", path)
		} else {
			raw += fmt.Sprintf("  session_secret: %q\n", secret)
		}
		return LoadReader(bytes.NewReader([]byte(raw)))
	}

	It("loads with no secret configured", func() {
		Expect(LoadReader(bytes.NewReader(nil))).To(Succeed())
	})

	DescribeTable("loads a secret at the floor",
		func(secret string, byFile bool) {
			Expect(load(secret, byFile)).To(Succeed())
			a := Get().Auth
			Expect(SecretValue(a.SessionSecret, a.SessionSecretFile)).
				To(Equal(atFloor))
		},
		Entry("inline", atFloor, false),
		Entry("in a file, trailing newline trimmed", atFloor+"\n", true),
	)

	// Not empty, and no more secret than the empty key: each is a value
	// anyone can guess from knowing how the file came to be written.
	DescribeTable("refuses a configured secret that is not usable",
		func(secret string, byFile bool, reason string) {
			err := load(secret, byFile)
			Expect(err).To(MatchError(ContainSubstring(reason)))
			if byFile {
				Expect(err).To(MatchError(ContainSubstring(
					`auth.session_secret_file "`,
				)))
			} else {
				Expect(err).To(MatchError(ContainSubstring(
					"auth.session_secret:",
				)))
			}
		},
		Entry("an empty file", "", true, "is empty"),
		Entry("a file of whitespace", " \n\t\n", true, "is empty"),
		Entry("a file holding a UTF-8 byte-order mark",
			"\xef\xbb\xbf", true, "shorter than"),
		Entry("an empty UTF-16 file as PowerShell 5 writes it",
			"\xff\xfe\r\x00\n\x00", true, "shorter than"),
		Entry("a placeholder in a file", "changeme\n", true, "shorter than"),
		Entry("an inline lone space", " ", false, "is empty"),
		Entry("an inline placeholder", "changeme", false, "shorter than"),
		Entry("inline, one byte under the floor",
			atFloor[1:], false, "shorter than"),
		Entry("inline, padded to the floor with whitespace",
			atFloor[1:]+" ", false, "shorter than"),
	)

	It("refuses an update that shortens the secret", func() {
		dir := GinkgoT().TempDir()
		path := writeConfigFileIn(dir, map[string]any{
			"data_dir": dir,
			"auth":     map[string]any{"session_secret": atFloor},
		})
		_, err := Load(path)
		Expect(err).ToNot(HaveOccurred())

		err = Update(context.Background(), func(c *Config) error {
			c.Auth.SessionSecret = "changeme"
			return nil
		})
		Expect(err).To(MatchError(ErrWriteBackUnloadable))
		Expect(err).To(MatchError(ContainSubstring("auth.session_secret:")))
		Expect(Get().Auth.SessionSecret).To(Equal(atFloor))
	})
})
