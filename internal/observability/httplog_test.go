package observability

import (
	"os"
	"path/filepath"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewHTTPLogger", Label("unit", "observability"), func() {
	defaultRotate := config.LogRotate{
		MaxSizeMB:  100,
		MaxBackups: 5,
		MaxAgeDays: 30,
		Compress:   true,
	}

	It("returns nil Logger when disabled", func() {
		cfg := config.HTTPLog{
			Enabled: false,
			Output:  "stderr",
			Format:  "json",
			Rotate:  defaultRotate,
		}
		l, err := NewHTTPLogger(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(l).To(BeNil())
	})

	It("rejects invalid format", func() {
		cfg := config.HTTPLog{
			Enabled: true,
			Output:  "stderr",
			Format:  "yaml",
			Rotate:  defaultRotate,
		}
		_, err := NewHTTPLogger(cfg)
		Expect(err).To(MatchError(ContainSubstring("format")))
	})

	It("accepts a relative output path (resolved under data_dir)", func() {
		configtest.Setup()
		cfg := config.HTTPLog{
			Enabled: true,
			Output:  "relative.log",
			Format:  "json",
			Rotate:  defaultRotate,
		}
		l, err := NewHTTPLogger(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(l).NotTo(BeNil())
		Expect(l.Close()).To(Succeed())
	})

	It("constructs with stderr output", func() {
		cfg := config.HTTPLog{
			Enabled: true,
			Output:  "stderr",
			Format:  "json",
			Rotate:  defaultRotate,
		}
		l, err := NewHTTPLogger(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(l).NotTo(BeNil())
		Expect(l.Close()).To(Succeed())
	})

	It("constructs with file output via timberjack", func() {
		dir, err := os.MkdirTemp("", "httplog-test-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(os.RemoveAll(dir)).To(Succeed()) })

		path := filepath.Join(dir, "access.log")
		cfg := config.HTTPLog{
			Enabled: true,
			Output:  path,
			Format:  "json",
			Rotate:  defaultRotate,
		}
		l, err := NewHTTPLogger(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(l).NotTo(BeNil())
		Expect(l.Close()).To(Succeed())
	})

	It("constructs with combined format", func() {
		cfg := config.HTTPLog{
			Enabled: true,
			Output:  "stderr",
			Format:  "combined",
			Rotate:  defaultRotate,
		}
		l, err := NewHTTPLogger(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(l).NotTo(BeNil())
		Expect(l.Close()).To(Succeed())
	})
})
