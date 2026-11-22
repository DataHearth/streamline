package web

import (
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The phone shell's bottom nav gives way to the on-screen keyboard
// (lib/keyboard.svelte.ts). Headless Chrome draws no keyboard: a viewport made
// shorter while a field has focus is what the real one does to the page under
// index.html's `interactive-widget=resizes-content`.
var _ = Describe("On-screen keyboard", Label("e2e"), func() {
	const (
		width    = 390
		height   = 844
		keyboard = 300
		field    = `main input:not([type=checkbox]):not([type=radio]):not([type=hidden])`
	)

	// phone opens a settings page holding a text field at a phone's size, with
	// a touch screen or with a mouse alone.
	phone := func(touch bool) *rod.Page {
		GinkgoHelper()
		page := newSessionPage("/settings/general")
		points := 5
		Expect(proto.EmulationSetTouchEmulationEnabled{
			Enabled:        touch,
			MaxTouchPoints: &points,
		}.Call(page)).To(Succeed())
		page.MustSetViewport(width, height, 1, touch)
		page.MustReload()
		page.MustElement(field)
		return page
	}

	navShown := func(page *rod.Page) func() bool {
		return func() bool {
			return page.MustEval(`() => getComputedStyle(
				document.querySelector("nav.fixed.bottom-0")).display !== "none"`).Bool()
		}
	}

	settle := func(page *rod.Page, shown bool) {
		GinkgoHelper()
		Eventually(navShown(page)).
			WithTimeout(3 * time.Second).
			WithPolling(50 * time.Millisecond).
			Should(Equal(shown))
	}

	It("hides the bottom nav while the keyboard is up on a touch screen", func() {
		page := phone(true)
		settle(page, true)

		page.MustElement(field).MustFocus()
		settle(page, true)

		page.MustSetViewport(width, height-keyboard, 1, true)
		settle(page, false)

		// Back closes the keyboard and leaves the field focused.
		page.MustSetViewport(width, height, 1, true)
		settle(page, true)
	})

	It("keeps the bottom nav when a mouse-driven window is made shorter", func() {
		page := phone(false)
		page.MustElement(field).MustFocus()
		page.MustSetViewport(width, height-keyboard, 1, false)
		Consistently(navShown(page)).
			WithTimeout(500 * time.Millisecond).
			WithPolling(50 * time.Millisecond).
			Should(BeTrue())
	})
})
