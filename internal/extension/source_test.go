package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

func sumHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func serveTLS(body []byte, hits *int) *httptest.Server {
	GinkgoHelper()
	srv := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			*hits++
			_, _ = w.Write(body)
		}),
	)
	prev := otelx.HTTPClient.Transport
	otelx.HTTPClient.Transport = srv.Client().Transport
	DeferCleanup(func() {
		otelx.HTTPClient.Transport = prev
		srv.Close()
	})
	return srv
}

var _ = Describe("resolveSource", Label("unit"), func() {
	var (
		ctx  = context.Background()
		dir  string
		wasm = []byte("\x00asm-not-really")
	)

	BeforeEach(func() { dir = filepath.Join(GinkgoT().TempDir(), "sources") })

	It("reads a local file and checks its sha256 when one is set", func() {
		path := filepath.Join(GinkgoT().TempDir(), "x.wasm")
		Expect(os.WriteFile(path, wasm, 0o600)).To(Succeed())

		got, err := resolveSource(ctx, config.ExtensionEntry{Source: path}, dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(wasm))

		_, err = resolveSource(
			ctx,
			config.ExtensionEntry{Source: path, SHA256: sumHex([]byte("other"))},
			dir,
		)
		Expect(err).To(MatchError(ContainSubstring("sha256")))
	})

	It("fetches over https once and then reads its kept copy", func() {
		hits := 0
		srv := serveTLS(wasm, &hits)
		e := config.ExtensionEntry{Source: srv.URL, SHA256: sumHex(wasm)}

		got, err := resolveSource(ctx, e, dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(wasm))
		Expect(filepath.Join(dir, sumHex(wasm)+".wasm")).To(BeAnExistingFile())

		got, err = resolveSource(ctx, e, dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(wasm))
		Expect(hits).To(Equal(1))
	})

	It("refuses bytes that do not match and keeps nothing", func() {
		hits := 0
		srv := serveTLS(wasm, &hits)
		_, err := resolveSource(
			ctx,
			config.ExtensionEntry{Source: srv.URL, SHA256: sumHex([]byte("other"))},
			dir,
		)
		Expect(err).To(MatchError(ContainSubstring("sha256")))
		Expect(dir).NotTo(BeADirectory())
	})

	It("refuses a body over the limit", func() {
		hits := 0
		srv := serveTLS(make([]byte, MaxWasmBytes+1), &hits)
		_, err := resolveSource(
			ctx,
			config.ExtensionEntry{Source: srv.URL, SHA256: sumHex(nil)},
			dir,
		)
		Expect(err).To(MatchError(errWasmTooLarge))
	})

	It("refuses http", func() {
		_, err := resolveSource(
			ctx,
			config.ExtensionEntry{
				Source: "http://example.com/x.wasm",
				SHA256: sumHex(nil),
			},
			dir,
		)
		Expect(err).To(MatchError(ContainSubstring("https")))
	})
})
