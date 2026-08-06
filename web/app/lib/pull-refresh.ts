// Pull down at the top of a list to re-fetch. The queue already repolls every
// 2 s, so this is a reassurance gesture rather than a data one — but a live
// screen with no way to ask "is that really current?" reads as stuck.
//
// Touch only, and only while the scroller is already at the top: a pull that
// starts mid-list belongs to the list. `touchmove` is non-passive so the pull
// can claim the gesture, but preventDefault fires only once the pull is real,
// which keeps normal scrolling on the browser's own path.

const TRIGGER = 64; // px of travel that commits a refresh
const MAX = 96; // px the indicator can be dragged to
const RESIST = 0.5; // half of the finger's travel, so the pull feels held

type Params = {
	onRefresh: () => unknown | Promise<unknown>;
	disabled?: boolean;
};

export function pullRefresh(node: HTMLElement, params: Params) {
	let opts = params;
	// The page's own scroller. AppShell puts every route inside #main.
	const scroller = node.closest<HTMLElement>("#main") ?? null;
	let startY = 0;
	let pull = 0;
	let active = false;
	let refreshing = false;

	const setPull = (v: number) => {
		pull = v;
		node.style.setProperty("--pull", `${Math.round(v)}px`);
		if (v > 0) node.dataset.pulling = "";
		else delete node.dataset.pulling;
		if (v >= TRIGGER && !refreshing) node.dataset.pullArmed = "";
		else delete node.dataset.pullArmed;
	};

	const clear = () => {
		active = false;
		node.style.transition = `transform var(--dur-base, 200ms) var(--ease, ease-out)`;
		node.style.transform = "";
		setPull(0);
		delete node.dataset.pulling;
		delete node.dataset.pullArmed;
	};

	const onStart = (e: TouchEvent) => {
		if (opts.disabled || refreshing || e.touches.length !== 1) return;
		if ((scroller?.scrollTop ?? 0) > 0) return;
		startY = e.touches[0].clientY;
		active = true;
	};

	const onMove = (e: TouchEvent) => {
		if (!active) return;
		const dy = e.touches[0].clientY - startY;
		if (dy <= 0) {
			// Turned into a scroll up; hand the gesture back.
			if (pull > 0) clear();
			active = false;
			return;
		}
		if ((scroller?.scrollTop ?? 0) > 0) {
			active = false;
			return;
		}
		e.preventDefault();
		node.style.transition = "none";
		const travel = Math.min(MAX, dy * RESIST);
		node.style.transform = `translate3d(0, ${travel}px, 0)`;
		setPull(travel);
	};

	const onEnd = async () => {
		if (!active) return;
		const committed = pull >= TRIGGER;
		if (!committed) {
			clear();
			return;
		}
		// Hold the indicator at the trigger point until the refetch settles.
		active = false;
		refreshing = true;
		node.dataset.refreshing = "";
		node.style.transition = `transform var(--dur-base, 200ms) var(--ease, ease-out)`;
		node.style.transform = `translate3d(0, ${TRIGGER}px, 0)`;
		setPull(TRIGGER);
		try {
			await opts.onRefresh();
		} catch {
			// A failed refetch surfaces through the query's own error state.
		} finally {
			refreshing = false;
			delete node.dataset.refreshing;
			clear();
		}
	};

	node.addEventListener("touchstart", onStart, { passive: true });
	node.addEventListener("touchmove", onMove, { passive: false });
	node.addEventListener("touchend", onEnd, { passive: true });
	node.addEventListener("touchcancel", clear, { passive: true });

	return {
		update(next: Params) {
			opts = next;
		},
		destroy() {
			node.removeEventListener("touchstart", onStart);
			node.removeEventListener("touchmove", onMove);
			node.removeEventListener("touchend", onEnd);
			node.removeEventListener("touchcancel", clear);
		},
	};
}
