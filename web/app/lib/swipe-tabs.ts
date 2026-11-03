// Swipe between a detail page's tabs on a phone or tablet: a horizontal flick
// over the tab's content moves to the next tab (leftwards) or the previous one
// (rightwards), as the tab bar would. Touch and pen only, and only below the
// `lg` breakpoint — a mouse and a desktop layout have the bar itself.
//
// The listeners are passive, so a vertical scroll is never held up: the
// gesture is judged once the finger lifts. It is ignored when it starts on
// something that already answers a horizontal drag (a scrolling row, a range
// input, a text field, anything marked `data-no-swipe`), or within the strip
// along either screen edge that iOS and Android keep for back navigation.
//
// While mounted at a touch width it also sets `overscroll-behavior-x: none` on
// the root element, or the browser reads the same flick as its own overscroll
// back/forward navigation — Chromium did exactly that on a rightward swipe,
// leaving the page instead of changing tabs. It has to be the root: the
// navigation is the viewport's, and the same value on `#main`, the app's
// actual scroller, did not stop it.

export type SwipeTabsOptions<K extends string> = {
	keys: readonly K[];
	current: K;
	onChange: (key: K) => void;
	// The tab bar's scrolling track, so the tab a swipe lands on is scrolled
	// into view when the bar is wider than the screen.
	track?: HTMLElement | null;
	disabled?: boolean;
};

const MIN_DISTANCE = 60;
const MAX_DURATION_MS = 700;
// Horizontal travel must dominate: a diagonal scroll is still a scroll.
const DOMINANCE = 1.6;
const EDGE_GUARD = 24;
const TOUCH_LAYOUT = "(max-width: 1023.98px)";

function claimsHorizontalDrag(target: EventTarget | null, root: HTMLElement): boolean {
	let el = target instanceof Element ? target : null;
	while (el && el !== root.parentElement) {
		if (el.closest("input, textarea, select, [contenteditable], [data-no-swipe]")) return true;
		if (el instanceof HTMLElement && el.scrollWidth > el.clientWidth + 1) {
			const ox = getComputedStyle(el).overflowX;
			if (ox === "auto" || ox === "scroll") return true;
		}
		el = el.parentElement;
	}
	return false;
}

/** Index of the tab a swipe of `dx` pixels lands on, or -1 for none. */
export function swipeTarget(count: number, index: number, dx: number): number {
	if (index < 0) return -1;
	const next = dx < 0 ? index + 1 : index - 1;
	return next >= 0 && next < count ? next : -1;
}

export function swipeTabs<K extends string>(node: HTMLElement, initial: SwipeTabsOptions<K>) {
	let opts = initial;
	let start: { x: number; y: number; t: number } | null = null;

	function begin(e: TouchEvent) {
		start = null;
		const p = e.touches[0];
		if (opts.disabled || !p || e.touches.length !== 1) return;
		if (!layout.matches) return;
		if (p.clientX < EDGE_GUARD || p.clientX > window.innerWidth - EDGE_GUARD) return;
		if (claimsHorizontalDrag(e.target, node)) return;
		start = { x: p.clientX, y: p.clientY, t: e.timeStamp };
	}

	function end(e: TouchEvent) {
		const s = start;
		start = null;
		const p = e.changedTouches[0];
		if (!s || !p) return;
		const dx = p.clientX - s.x;
		const dy = p.clientY - s.y;
		if (e.timeStamp - s.t > MAX_DURATION_MS) return;
		if (Math.abs(dx) < MIN_DISTANCE || Math.abs(dx) < Math.abs(dy) * DOMINANCE) return;
		// A text selection being dragged is not a swipe.
		if (window.getSelection()?.toString()) return;
		const next = opts.keys[swipeTarget(opts.keys.length, opts.keys.indexOf(opts.current), dx)];
		if (next === undefined) return;
		opts.onChange(next);
		requestAnimationFrame(() =>
			opts.track
				?.querySelector<HTMLElement>('[aria-current="page"]')
				?.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "smooth" }),
		);
	}

	function cancel() {
		start = null;
	}

	const layout = window.matchMedia(TOUCH_LAYOUT);
	const root = document.documentElement;
	const previous = root.style.overscrollBehaviorX;
	function holdOverscroll() {
		root.style.overscrollBehaviorX = layout.matches && !opts.disabled ? "none" : previous;
	}
	holdOverscroll();
	layout.addEventListener("change", holdOverscroll);

	node.addEventListener("touchstart", begin, { passive: true });
	node.addEventListener("touchend", end, { passive: true });
	node.addEventListener("touchcancel", cancel, { passive: true });

	return {
		update(next: SwipeTabsOptions<K>) {
			opts = next;
			holdOverscroll();
		},
		destroy() {
			layout.removeEventListener("change", holdOverscroll);
			root.style.overscrollBehaviorX = previous;
			node.removeEventListener("touchstart", begin);
			node.removeEventListener("touchend", end);
			node.removeEventListener("touchcancel", cancel);
		},
	};
}
