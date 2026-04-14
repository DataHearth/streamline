package config

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PublicURL", Label("unit", "config"), func() {
	BeforeEach(func() {
		DeferCleanup(func() {
			_ = os.Unsetenv("STREAMLINE_PUBLIC_URL")
			ResetForTest()
		})
	})

	It("returns the STREAMLINE_PUBLIC_URL env value when set", func() {
		Expect(os.Setenv("STREAMLINE_PUBLIC_URL", "https://stream.example.com")).
			To(Succeed())
		store(&Config{Server: ServerConfig{Host: "127.0.0.1", Port: 8080}}, "")

		Expect(PublicURL()).To(Equal("https://stream.example.com"))
	})

	It("falls back to http://host:port from config when env is unset", func() {
		_ = os.Unsetenv("STREAMLINE_PUBLIC_URL")
		store(&Config{Server: ServerConfig{Host: "10.0.0.5", Port: 9000}}, "")

		Expect(PublicURL()).To(Equal("http://10.0.0.5:9000"))
	})
})
