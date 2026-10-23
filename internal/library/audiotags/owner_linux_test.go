//go:build linux

package audiotags

import (
	"os"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
	"golang.org/x/sys/unix"
)

var _ = Describe("Write ownership", Label("unit", "audiotags"), func() {
	full := Tags{taglib.Title: {"Lithium"}}

	It("keeps the file's owner, group and extended attributes", func() {
		if os.Getuid() != 0 {
			Skip("giving a file away needs root")
		}
		path := copyFixture("testdata/tagged.flac")
		Expect(os.Chown(path, 4242, 4343)).To(Succeed())
		if err := unix.Setxattr(path, "user.origin", []byte("rip"), 0); err != nil {
			Skip("the temp filesystem keeps no user xattrs: " + err.Error())
		}

		Expect(Write(path, full, Policy{})).To(Succeed())

		st, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		sys := st.Sys().(*syscall.Stat_t)
		Expect([]uint32{sys.Uid, sys.Gid}).To(Equal([]uint32{4242, 4343}))
		buf := make([]byte, 16)
		n, err := unix.Getxattr(path, "user.origin", buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("rip"))
		Expect(rawTags(path)).To(HaveKeyWithValue(taglib.Title, []string{"Lithium"}))
	})
})
