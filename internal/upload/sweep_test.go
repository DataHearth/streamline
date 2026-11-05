package upload_test

import (
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/upload"
)

var _ = Describe("Sweep", Label("unit"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"library": map[string]any{"download_path": GinkgoT().TempDir()},
		})
	})

	It("removes what is neither kept nor being received", func() {
		root := upload.Root()
		for _, d := range []string{"kept", "stray", "other.partial"} {
			Expect(os.MkdirAll(filepath.Join(root, d), 0o700)).To(Succeed())
		}

		pr, pw := io.Pipe()
		DeferCleanup(func() { pw.CloseWithError(io.ErrClosedPipe) })
		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			_, err := upload.Receive(
				context.Background(),
				multipart.NewReader(pr, "b"),
				upload.KindAlbum,
			)
			Expect(err).To(HaveOccurred())
		}()
		Eventually(func() []string { return entries(root) }).Should(HaveLen(4))

		Expect(
			upload.Sweep(
				context.Background(),
				func(context.Context) ([]string, error) {
					return []string{filepath.Join(root, "kept")}, nil
				},
			),
		).To(Succeed())

		left := entries(root)
		Expect(left).To(HaveLen(2))
		Expect(left).To(ContainElement("kept"))
		Expect(left).NotTo(ContainElement("stray"))
		Expect(left).NotTo(ContainElement("other.partial"))

		pw.CloseWithError(io.ErrClosedPipe)
		<-done
		Expect(entries(root)).To(ConsistOf("kept"))
	})

	It("leaves an upload that starts after the listing", func() {
		root := upload.Root()
		Expect(os.MkdirAll(root, 0o700)).To(Succeed())
		Expect(
			upload.Sweep(
				context.Background(),
				func(context.Context) ([]string, error) {
					return nil, os.Mkdir(filepath.Join(root, "late"), 0o700)
				},
			),
		).To(Succeed())
		Expect(entries(root)).To(ConsistOf("late"))
	})
})
