package extension

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeSQL struct{}

func (fakeSQL) Query(context.Context, string, []any) (SQLResult, error) {
	return SQLResult{Columns: []string{"n"}, Rows: [][]any{{1}}}, nil
}

func (fakeSQL) Exec(context.Context, string, []any) (SQLResult, error) {
	return SQLResult{}, nil
}

var _ = Describe("Module", Label("unit"), func() {
	var m *Module

	handle := func(ctx context.Context, path string) (Response, error) {
		return m.Handle(ctx, fakeSQL{}, Request{Method: "GET", Path: path}, nil)
	}

	BeforeEach(func() {
		wasm, err := os.ReadFile("testdata/echo/echo.wasm")
		Expect(err).NotTo(HaveOccurred())
		m, err = Compile(context.Background(), wasm)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(m.Close(context.Background())).To(Succeed()) })
	})

	It("reads the manifest", func() {
		Expect(
			m.Manifest().Mounts,
		).To(Equal([]Mount{{Prefix: "/echo", Auth: "self"}}))
	})

	It("answers a request", func() {
		resp, err := handle(context.Background(), "/hi")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Status).To(Equal(200))
		Expect(string(resp.Body)).To(Equal("/hi"))
	})

	It("returns a directive untouched", func() {
		resp, err := handle(context.Background(), "/file")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.ServeFile).NotTo(BeNil())
		Expect(resp.ServeFile.FileID).To(Equal(uint32(7)))
	})

	It("reaches the SQL host call", func() {
		resp, err := handle(context.Background(), "/sql")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(resp.Body)).To(ContainSubstring(`"n"`))
	})

	It("gives each instance its own random bytes", func() {
		first, err := handle(context.Background(), "/random")
		Expect(err).NotTo(HaveOccurred())
		second, err := handle(context.Background(), "/random")
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Body).To(HaveLen(32))
		Expect(second.Body).NotTo(Equal(first.Body))
	})

	It("survives a trap", func() {
		_, err := handle(context.Background(), "/trap")
		Expect(err).To(MatchError(ErrGuestFailed))
		resp, err := handle(context.Background(), "/hi")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Status).To(Equal(200))
	})

	It("stops a guest at the deadline", func() {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			200*time.Millisecond,
		)
		defer cancel()
		start := time.Now()
		_, err := handle(ctx, "/spin")
		Expect(err).To(HaveOccurred())
		Expect(time.Since(start)).To(BeNumerically("<", 2*time.Second))
		resp, err := handle(context.Background(), "/hi")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Status).To(Equal(200))
	})

	It("stops a guest at the memory limit", func() {
		_, err := handle(context.Background(), "/grow")
		Expect(err).To(MatchError(ErrGuestFailed))
		resp, err := handle(context.Background(), "/hi")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Status).To(Equal(200))
	})
})

var _ = Describe("parseManifest", Label("unit"), func() {
	It("refuses another contract version", func() {
		_, err := parseManifest(
			[]byte(`{"name":"x","version":"1","streamline_api":2}`),
		)
		Expect(err).To(MatchError(ErrBadManifest))
	})

	It("refuses a bad name", func() {
		_, err := parseManifest(
			[]byte(`{"name":"Bad Name","version":"1","streamline_api":1}`),
		)
		Expect(err).To(MatchError(ErrBadManifest))
	})

	It("accepts a good manifest", func() {
		mf, err := parseManifest(
			[]byte(`{"name":"ok-1","version":"1","streamline_api":1}`),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(mf.Name).To(Equal("ok-1"))
	})
})
