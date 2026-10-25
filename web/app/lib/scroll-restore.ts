import { activeRoute } from "@roxi/routify";

// Back and Forward return to where a page was left. `#main` is the app's one
// scroller and outlives every route, and Routify scrolls it to the top after
// each route renders, so without this Back from a movie, or from the phone's
// search page, came up at the top of a list scrolled a hundred posters deep.
//
// Positions are keyed by the route's URL, query included: a list's filters are
// in its query string, and a different filter is a different list. A link or
// a replace (a list rewriting its own query) starts where Routify puts it;
// only a history traversal restores.
const KEEP = 50;
// Long enough to outlast Routify's own scroll-to-top, which runs a tick after
// the route renders, and the first paint of a cached list.
const SETTLE_MS = 1000;

export function restoreScrollOnBack(main: HTMLElement): () => void {
	const saved = new Map<string, number>();
	let current: string | null = null;
	let cancel: (() => void) | null = null;

	const off = activeRoute.subscribe((r) => {
		if (!r) return;
		cancel?.();
		cancel = null;
		// The outgoing page is still in the DOM: the new route is rendered on
		// the next flush, after this store update.
		if (current !== null) {
			saved.delete(current);
			saved.set(current, main.scrollTop);
			if (saved.size > KEEP) saved.delete(saved.keys().next().value as string);
		}
		current = r.url ?? "";
		if (r.mode !== "popState") return;
		const top = saved.get(current);
		if (top) cancel = settle(main, top);
	});

	return () => {
		cancel?.();
		off();
	};
}

// Keeps putting the scroller back at `top` until the window closes: the page
// may still be growing to that height, and Routify scrolls to the top once
// after rendering. Any input of the user's own ends it at once.
function settle(main: HTMLElement, top: number): () => void {
	const until = performance.now() + SETTLE_MS;
	let frame = 0;
	const stop = () => {
		cancelAnimationFrame(frame);
		for (const ev of INPUT) window.removeEventListener(ev, stop, true);
	};
	const step = () => {
		if (main.scrollTop !== top) main.scrollTo({ top, behavior: "instant" });
		if (performance.now() < until) frame = requestAnimationFrame(step);
		else stop();
	};
	for (const ev of INPUT) window.addEventListener(ev, stop, { capture: true, passive: true });
	frame = requestAnimationFrame(step);
	return stop;
}

const INPUT = ["wheel", "touchstart", "pointerdown", "keydown"] as const;
