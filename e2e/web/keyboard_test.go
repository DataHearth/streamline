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

	// phone opens a settings page at a phone's size, with a touch screen or
	// with a mouse alone.
	phone := func(path string, touch bool) *rod.Page {
		GinkgoHelper()
		page := newSessionPage(path)
		points := 5
		Expect(proto.EmulationSetTouchEmulationEnabled{
			Enabled:        touch,
			MaxTouchPoints: &points,
		}.Call(page)).To(Succeed())
		page.MustSetViewport(width, height, 1, touch)
		page.MustReload()
		page.MustElement("main input")
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
		page := phone("/settings/general", true)
		settle(page, true)

		page.MustElement(field).MustFocus()
		settle(page, true)

		page.MustSetViewport(width, height-keyboard, 1, true)
		settle(page, false)
		// Chrome shrinks the layout viewport itself: no inset on top of it.
		Expect(page.MustEval(`() => getComputedStyle(document.documentElement)
			.getPropertyValue("--keyboard-inset")`).Str()).To(Equal("0px"))

		// Back closes the keyboard and leaves the field focused.
		page.MustSetViewport(width, height, 1, true)
		settle(page, true)
	})

	It("keeps the bottom nav hidden while focus passes through a button", func() {
		page := phone("/settings/general", true)
		page.MustElement(field).MustFocus()
		page.MustSetViewport(width, height-keyboard, 1, true)
		settle(page, false)

		// A Select taking focus to its trigger and then, a microtask later, to
		// its own search field: the keyboard never closes.
		passed := page.MustEval(`(f) => {
			const input = document.querySelector(f);
			const button = document.createElement("button");
			button.textContent = "trigger";
			input.after(button);
			button.focus();
			const reached = document.activeElement === button;
			queueMicrotask(() => input.focus());
			return reached;
		}`, field).Bool()
		Expect(passed).To(BeTrue(), "focus reached the button")
		Consistently(navShown(page)).
			WithTimeout(500 * time.Millisecond).
			WithPolling(25 * time.Millisecond).
			Should(BeFalse())
	})

	It("brings the bottom nav back only once the keyboard is down", func() {
		page := phone("/settings/general", true)
		page.MustElement(field).MustFocus()
		page.MustSetViewport(width, height-keyboard, 1, true)
		settle(page, false)

		// A tap on nothing: focus leaves the field, the keyboard starts down.
		page.MustEval(`() => document.activeElement.blur()`)
		Consistently(navShown(page)).
			WithTimeout(300 * time.Millisecond).
			WithPolling(25 * time.Millisecond).
			Should(BeFalse())
		page.MustSetViewport(width, height-keyboard/2, 1, true)
		Consistently(navShown(page)).
			WithTimeout(300 * time.Millisecond).
			WithPolling(25 * time.Millisecond).
			Should(BeFalse())

		page.MustSetViewport(width, height, 1, true)
		settle(page, true)
	})

	It(
		"brings the bottom nav back when a shrink made while typing was no keyboard",
		func() {
			page := phone("/settings/general", true)
			page.MustElement(field).MustFocus()
			// A split screen made while typing reads as the keyboard at first.
			page.MustSetViewport(width, height-keyboard, 1, true)
			settle(page, false)

			// Focus leaves every field and the page never grows back.
			page.MustEval(`() => document.activeElement.blur()`)
			Consistently(navShown(page)).
				WithTimeout(500 * time.Millisecond).
				WithPolling(50 * time.Millisecond).
				Should(BeFalse())
			settle(page, true)
		},
	)

	It("keeps the bottom nav when a mouse-driven window is made shorter", func() {
		page := phone("/settings/general", false)
		page.MustElement(field).MustFocus()
		page.MustSetViewport(width, height-keyboard, 1, false)
		Consistently(navShown(page)).
			WithTimeout(500 * time.Millisecond).
			WithPolling(50 * time.Millisecond).
			Should(BeTrue())
	})

	It("keeps a searchable Select open over the keyboard it raises", func() {
		page := phone("/settings/library", true)
		// The book language Select, scrolled to where the keyboard will cover it.
		trigger := page.MustElementByJS(`() => {
			const label = [...document.querySelectorAll("main *")].find((el) =>
				el.childElementCount === 0 &&
				el.textContent.trim() === "Preferred book language");
			const button = label.closest("div.max-w-xs")
				.querySelector("button[aria-haspopup=listbox]");
			const main = document.getElementById("main");
			main.scrollTop += button.getBoundingClientRect().top - 600;
			return button;
		}`)
		searching := func() bool {
			return page.MustEval(`() =>
				document.activeElement?.matches("input[type=search]") ?? false`).Bool()
		}

		trigger.MustClick()
		Eventually(searching).
			WithTimeout(3 * time.Second).
			WithPolling(50 * time.Millisecond).
			Should(BeTrue())

		page.MustSetViewport(width, height-keyboard, 1, true)
		Consistently(searching).
			WithTimeout(time.Second).
			WithPolling(50 * time.Millisecond).
			Should(BeTrue())
		Expect(trigger.MustEval(`function () {
			const r = this.getBoundingClientRect();
			return r.top >= 0 && r.bottom <= window.innerHeight;
		}`).Bool()).To(BeTrue(), "the trigger is back above the keyboard")
	})

	It("still closes a menu with focus inside it when the page scrolls", func() {
		page := newSessionPage("/settings/library")
		page.MustElement("main input")
		trigger := page.MustElementByJS(`() => {
			const label = [...document.querySelectorAll("main *")].find((el) =>
				el.childElementCount === 0 &&
				el.textContent.trim() === "Preferred book language");
			return label.closest("div.max-w-xs")
				.querySelector("button[aria-haspopup=listbox]");
		}`)
		trigger.MustClick()
		page.MustElement(`input[type=search]`)

		page.MustEval(`() => {
			const main = document.getElementById("main");
			main.scrollTop = 0;
			main.scrollTop = main.scrollHeight;
		}`)
		Eventually(func() bool {
			return page.MustHas(`input[type=search]`)
		}).
			WithTimeout(3 * time.Second).
			WithPolling(50 * time.Millisecond).
			Should(BeFalse())
	})
})
