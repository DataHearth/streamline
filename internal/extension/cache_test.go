package extension

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("sweepDir", Label("unit"), func() {
	var dir string

	put := func(name string, age time.Duration) string {
		GinkgoHelper()
		p := filepath.Join(dir, name)
		Expect(os.WriteFile(p, make([]byte, 10), 0o600)).To(Succeed())
		when := time.Now().Add(-age)
		Expect(os.Chtimes(p, when, when)).To(Succeed())
		return p
	}

	BeforeEach(func() { dir = GinkgoT().TempDir() })

	It("deletes the oldest file when the folder is over the limit", func() {
		oldest := put("a", 3*time.Hour)
		mid := put("b", 2*time.Hour)
		newest := put("c", time.Hour)

		Expect(sweepDir(context.Background(), dir, 20)).To(Succeed())

		Expect(oldest).NotTo(BeAnExistingFile())
		Expect(mid).To(BeARegularFile())
		Expect(newest).To(BeARegularFile())
	})

	It("leaves a folder under the limit alone", func() {
		a := put("a", 2*time.Hour)
		b := put("b", time.Hour)

		Expect(sweepDir(context.Background(), dir, 20)).To(Succeed())

		Expect(a).To(BeARegularFile())
		Expect(b).To(BeARegularFile())
	})
})
