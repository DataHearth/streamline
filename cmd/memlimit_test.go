package main

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cgroupMemoryLimit", Label("unit", "cli"), func() {
	// write lays out one cgroup file under a temp root and returns the root.
	write := func(root, rel, content string) {
		GinkgoHelper()
		p := filepath.Join(root, rel)
		Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		Expect(os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
	}

	It("reads the v2 limit at the mount root, as a container sees it", func() {
		root := GinkgoT().TempDir()
		write(root, "memory.max", "268435456\n")

		Expect(cgroupMemoryLimit(root, filepath.Join(root, "missing"))).
			To(Equal(int64(268435456)))
	})

	It("reads the v1 limit", func() {
		root := GinkgoT().TempDir()
		write(root, "memory/memory.limit_in_bytes", "134217728\n")

		Expect(cgroupMemoryLimit(root, filepath.Join(root, "missing"))).
			To(Equal(int64(134217728)))
	})

	It("treats an unset limit as no limit, in both layouts", func() {
		root := GinkgoT().TempDir()
		write(root, "memory.max", "max\n")
		write(root, "memory/memory.limit_in_bytes", "9223372036854771712\n")

		Expect(cgroupMemoryLimit(root, filepath.Join(root, "missing"))).
			To(BeZero())
	})

	It("returns zero when no cgroup files exist at all", func() {
		root := GinkgoT().TempDir()

		Expect(cgroupMemoryLimit(root, filepath.Join(root, "missing"))).
			To(BeZero())
	})

	It(
		"walks the process's own path up to an ancestor slice holding the limit",
		func() {
			root := GinkgoT().TempDir()
			procSelf := filepath.Join(GinkgoT().TempDir(), "cgroup")
			Expect(os.WriteFile(
				procSelf, []byte("0::/system.slice/streamline.service\n"), 0o644,
			)).To(Succeed())
			write(root, "system.slice/memory.max", "536870912\n")

			Expect(cgroupMemoryLimit(root, procSelf)).To(Equal(int64(536870912)))
		},
	)

	It("takes the smallest ceiling when several apply", func() {
		root := GinkgoT().TempDir()
		procSelf := filepath.Join(GinkgoT().TempDir(), "cgroup")
		Expect(os.WriteFile(
			procSelf, []byte("0::/system.slice/streamline.service\n"), 0o644,
		)).To(Succeed())
		write(root, "system.slice/memory.max", "536870912\n")
		write(root, "system.slice/streamline.service/memory.max", "268435456\n")

		Expect(cgroupMemoryLimit(root, procSelf)).To(Equal(int64(268435456)))
	})
})

var _ = Describe("selfCgroupPaths", Label("unit", "cli"), func() {
	It("takes the v2 unified line and any v1 line carrying memory", func() {
		p := filepath.Join(GinkgoT().TempDir(), "cgroup")
		Expect(os.WriteFile(p, []byte(
			"12:cpuset:/docker/abc\n"+
				"5:memory,cpu:/docker/def\n"+
				"0::/system.slice/app.service\n",
		), 0o644)).To(Succeed())

		Expect(selfCgroupPaths(p)).To(ConsistOf(
			"/docker/def", "/system.slice/app.service",
		))
	})

	It("returns nothing when the file is absent", func() {
		Expect(selfCgroupPaths("/nonexistent/cgroup")).To(BeEmpty())
	})
})
