package server

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("finishChangesDuringRestore", Label("unit", "server"), func() {
	// serve reports whether the handler's context could still be cancelled.
	serve := func(ready <-chan struct{}, method, path string) bool {
		GinkgoHelper()
		var cancellable bool
		h := finishChangesDuringRestore(ready)(http.HandlerFunc(
			func(_ http.ResponseWriter, r *http.Request) {
				cancellable = r.Context().Done() != nil
			},
		))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req := httptest.NewRequestWithContext(ctx, method, path, nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
		return cancellable
	}

	It("detaches a change made while the engine restores", func() {
		Expect(
			serve(make(chan struct{}), http.MethodDelete, "/api/v1/x"),
		).To(BeFalse())
	})

	It("keeps a read cancellable while the engine restores", func() {
		Expect(serve(make(chan struct{}), http.MethodGet, "/api/v1/x")).To(BeTrue())
	})

	It("keeps a Subsonic POST cancellable while the engine restores", func() {
		Expect(
			serve(make(chan struct{}), http.MethodPost, "/rest/search3.view"),
		).To(BeTrue())
	})

	It("keeps a change cancellable once the engine is ready", func() {
		ready := make(chan struct{})
		close(ready)
		Expect(serve(ready, http.MethodPost, "/api/v1/x")).To(BeTrue())
	})
})
