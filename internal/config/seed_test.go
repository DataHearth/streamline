package config

import (
	"bytes"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Seed", Label("unit", "config"), func() {
	BeforeEach(func() {
		ResetForTest()
	})

	It(
		"writes the config init defaults when the file is missing, and Load accepts them",
		func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "config", "config.yaml")
			GinkgoT().Setenv("STREAMLINE_DATA_DIR", filepath.Join(dir, "data"))

			seeded, err := Seed(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(seeded).To(BeTrue())

			info, err := os.Stat(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

			cfg, err := Load(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Library.MoviePath).To(Equal(defaults()["library.movie_path"]))
			Expect(Path()).To(Equal(path))
		},
	)

	It(
		"takes the library folders and data_dir from the environment, and no secret",
		func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "config.yaml")
			GinkgoT().Setenv("STREAMLINE_DATA_DIR", filepath.Join(dir, "data"))
			GinkgoT().Setenv("STREAMLINE_LIBRARY__MOVIE_PATH", "/mnt/user/data/media/movies")
			GinkgoT().Setenv("STREAMLINE_AUTH__SESSION_SECRET", "env-secret")
			GinkgoT().Setenv("STREAMLINE_SERVER__PORT", "7070")

			_, err := Seed(path)
			Expect(err).ToNot(HaveOccurred())

			raw, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(
				string(raw),
			).To(ContainSubstring("movie_path: /mnt/user/data/media/movies"))
			Expect(
				string(raw),
			).To(ContainSubstring("data_dir: " + filepath.Join(dir, "data")))
			Expect(string(raw)).ToNot(ContainSubstring("env-secret"))
			Expect(string(raw)).To(ContainSubstring("port: 8080"))
		},
	)

	It("matches config init byte for byte with no environment", func() {
		path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
		_, err := Seed(path)
		Expect(err).ToNot(HaveOccurred())

		var want bytes.Buffer
		Expect(DumpDefaults(&want)).To(Succeed())
		raw, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(raw).To(Equal(want.Bytes()))
	})

	It("leaves an existing file alone, even an empty one", func() {
		path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
		Expect(os.WriteFile(path, nil, 0o644)).To(Succeed())

		seeded, err := Seed(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(seeded).To(BeFalse())
		raw, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(raw).To(BeEmpty())
	})

	It("leaves a dangling symlink for Load to report", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "config.yaml")
		Expect(os.Symlink(filepath.Join(dir, "missing.yaml"), path)).To(Succeed())

		seeded, err := Seed(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(seeded).To(BeFalse())
		_, err = os.Stat(filepath.Join(dir, "missing.yaml"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("does nothing without a path", func() {
		seeded, err := Seed("")
		Expect(err).ToNot(HaveOccurred())
		Expect(seeded).To(BeFalse())
	})

	It("names the fix when the directory is not writable", func() {
		if os.Geteuid() == 0 {
			Skip("root writes through directory permissions")
		}
		dir := GinkgoT().TempDir()
		Expect(os.Chmod(dir, 0o500)).To(Succeed())
		DeferCleanup(os.Chmod, dir, os.FileMode(0o700))
		path := filepath.Join(dir, "config", "config.yaml")

		seeded, err := Seed(path)
		Expect(seeded).To(BeFalse())
		Expect(err).To(MatchError(And(
			ContainSubstring("does not exist and cannot be created"),
			ContainSubstring("config init --output "+path),
		)))
	})
})
