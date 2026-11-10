package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// pixel is a 1x1 transparent GIF.
const pixel = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=="

var _ = Describe("brokenImages", Label("e2e", "demoshots"), func() {
	It("names the images in view that loaded nothing", func(ctx SpecContext) {
		bin := chromeBin()
		if bin == "" {
			Skip("no Chromium (CHROME_PATH or on PATH)")
		}
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				_, _ = fmt.Fprintf(w, `<!doctype html><body style="margin:0">
					<img id="ok" src=%q width="40" height="40">
					<img id="gone" src="/posters/gone.jpg" width="40" height="40">
					<img id="below" src="/posters/below.jpg" width="40" height="40"
						style="position:absolute;top:5000px">`, pixel)
			}),
		)
		DeferCleanup(srv.Close)

		l := launcher.New().Bin(bin).Headless(true).NoSandbox(os.Geteuid() == 0)
		DeferCleanup(l.Kill)
		ctrl, err := l.Launch()
		Expect(err).NotTo(HaveOccurred())
		browser := rod.New().ControlURL(ctrl).Context(ctx)
		Expect(browser.Connect()).To(Succeed())
		// The spec's context is already cancelled when cleanup runs.
		DeferCleanup(func() { _ = browser.Close() })

		page, err := (&demo{}).newPage(browser, 800, 600, 1, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Navigate(srv.URL)).To(Succeed())
		Expect(page.WaitLoad()).To(Succeed())

		By("an image that 404s is complete, which is all settle looks at")
		res, err := page.Eval(`() => document.getElementById("gone").complete`)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Value.Bool()).To(BeTrue())

		Expect(brokenImages(page)).To(ConsistOf(srv.URL + "/posters/gone.jpg"))
	})
})
