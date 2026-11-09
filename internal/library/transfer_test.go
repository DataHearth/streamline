package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("transferFile", Label("unit", "library"), func() {
	var tmp, src, dst string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
		src = filepath.Join(tmp, "src.mkv")
		dst = filepath.Join(tmp, "dst.mkv")
		Expect(os.WriteFile(src, []byte("bytes"), 0o600)).To(Succeed())
	})

	It("hardlinks the file", func() {
		Expect(transferFile(src, dst, "hardlink")).To(Succeed())

		srcStat, err := os.Stat(src)
		Expect(err).NotTo(HaveOccurred())
		dstStat, err := os.Stat(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.SameFile(srcStat, dstStat)).To(BeTrue())
	})

	It("moves the file", func() {
		Expect(transferFile(src, dst, "move")).To(Succeed())

		_, err := os.Stat(src)
		Expect(os.IsNotExist(err)).To(BeTrue())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
	})

	It("copies the file preserving source", func() {
		Expect(transferFile(src, dst, "copy")).To(Succeed())

		_, err := os.Stat(src)
		Expect(err).NotTo(HaveOccurred())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
	})

	It("rejects an unknown import mode", func() {
		err := transferFile(src, dst, "teleport")
		Expect(err).To(MatchError(ContainSubstring("unknown import mode")))
	})

	It("returns error when the source is missing (hardlink)", func() {
		err := transferFile(filepath.Join(tmp, "missing"), dst, "hardlink")
		Expect(err).To(HaveOccurred())
	})

	It("returns error when copy cannot open the source", func() {
		err := transferFile(filepath.Join(tmp, "missing"), dst, "copy")
		Expect(err).To(HaveOccurred())
	})

	It("returns error when copy cannot create the destination dir", func() {
		err := transferFile(src, filepath.Join(tmp, "missing-dir", "dst"), "copy")
		Expect(err).To(HaveOccurred())
	})
})

// A rename across two mounts cannot be staged in a unit spec, so the seams
// stand in for the kernel: renameFile answers EXDEV whenever the two paths sit
// in different directories, which is what a library on its own filesystem
// looks like from here. The rename of the staged copy into place stays inside
// one directory and is os.Rename itself.
var _ = Describe("moving across filesystems", Label("unit", "library"), func() {
	var tmp, src, dstDir, dst string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
		src = filepath.Join(tmp, "src.mkv")
		dstDir = filepath.Join(tmp, "library")
		dst = filepath.Join(dstDir, "dst.mkv")
		Expect(os.MkdirAll(dstDir, 0o750)).To(Succeed())
		Expect(os.WriteFile(src, []byte("bytes"), 0o640)).To(Succeed())

		origRename, origCopy := renameFile, copyBytes
		DeferCleanup(func() { renameFile, copyBytes = origRename, origCopy })
		renameFile = func(oldpath, newpath string) error {
			if filepath.Dir(oldpath) != filepath.Dir(newpath) {
				return &os.LinkError{
					Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV,
				}
			}
			return os.Rename(oldpath, newpath)
		}
	})

	entries := func() []string {
		GinkgoHelper()
		list, err := os.ReadDir(dstDir)
		Expect(err).NotTo(HaveOccurred())
		names := make([]string, 0, len(list))
		for _, e := range list {
			names = append(names, e.Name())
		}
		return names
	}

	It("copies the file over, keeps its mode and removes the source", func() {
		Expect(transferFile(src, dst, "move")).To(Succeed())

		Expect(src).NotTo(BeAnExistingFile())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
		info, err := os.Stat(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o640)))
		Expect(entries()).To(ConsistOf("dst.mkv"))
	})

	It("does the same for MoveFile, creating the parent", func() {
		nested := filepath.Join(dstDir, "Movie (2020)", "dst.mkv")
		Expect(MoveFile(context.Background(), src, nested)).To(Succeed())

		Expect(src).NotTo(BeAnExistingFile())
		contents, err := os.ReadFile(nested)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
		siblings, err := os.ReadDir(filepath.Dir(nested))
		Expect(err).NotTo(HaveOccurred())
		Expect(siblings).To(HaveLen(1))
	})

	It("keeps the source and leaves nothing behind when the copy fails", func() {
		copyBytes = func(w io.Writer, _ io.Reader) (int64, error) {
			n, _ := w.Write([]byte("by"))
			return int64(n), errors.New("no space left on device")
		}

		err := transferFile(src, dst, "move")
		Expect(err).To(MatchError(ContainSubstring("no space left on device")))

		contents, readErr := os.ReadFile(src)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
		Expect(entries()).To(BeEmpty())
	})

	It("leaves an existing destination untouched when the copy fails", func() {
		Expect(os.WriteFile(dst, []byte("previous"), 0o600)).To(Succeed())
		copyBytes = func(io.Writer, io.Reader) (int64, error) {
			return 0, errors.New("input/output error")
		}

		Expect(transferFile(src, dst, "move")).NotTo(Succeed())

		Expect(src).To(BeAnExistingFile())
		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("previous"))
		Expect(entries()).To(ConsistOf("dst.mkv"))
	})

	It("overwrites a staged copy a crashed run left behind", func() {
		Expect(os.WriteFile(stagingPath(dst), []byte("stale-and-longer"), 0o600)).
			To(Succeed())

		Expect(transferFile(src, dst, "move")).To(Succeed())

		contents, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("bytes"))
		Expect(entries()).To(ConsistOf("dst.mkv"))
	})

	It("does not fall back on a rename error other than EXDEV", func() {
		renameFile = func(string, string) error { return syscall.EACCES }

		Expect(transferFile(src, dst, "move")).To(MatchError(syscall.EACCES))
		Expect(src).To(BeAnExistingFile())
		Expect(entries()).To(BeEmpty())
	})
})
