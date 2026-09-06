// Drag a bottom sheet down to dismiss it. Shared by the library filter sheet
// and the bulk-action sheet.
//
// Transform writes go straight to the node: animating anything that relayouts
// costs a frame per pointermove on mobile Gecko, which is the whole reason the
// add sheet moved to a pure-transform drag. The gesture only starts from the
// header or from a list already scrolled to the top — otherwise it belongs to
// the list.
//
// On touch the pointer stream is not enough on its own: a touch the browser is
// allowed to interpret becomes a scroll, and it cancels the pointer the moment
// it decides that — so the drag used to work only from the header, the one strip
// carrying `touch-action: none`. `touch-action` cannot express "mine only
// downward, and only from a list already at the top", so the touch move is
// claimed in JS, the way LookupSheet claims its own.

const DISMISS_RATIO = 0.3;
const FLICK = 0.5; // px per ms
const SLOP = 6; // a tap on a row must stay a tap

export function sheetSwipe(
	node: HTMLElement,
	params: { onDismiss: () => void; disabled?: boolean },
) {
	let onDismiss = params.onDismiss;
	let disabled = params.disabled ?? false;
	let id: number | null = null;
	let startY = 0;
	let startX = 0;
	let startedAt = 0;
	let dy = 0;
	let dragging = false;
	// Both streams describe the same finger. Whichever claims the gesture first
	// drives it; the other stands down rather than painting the same frame twice.
	let touchDriven = false;

	const reset = (animate: boolean) => {
		node.style.transition = animate
			? "transform var(--dur-base, 200ms) var(--ease, ease-out)"
			: "";
		node.style.transform = "";
	};

	const drive = (delta: number) => {
		if (!dragging) {
			dragging = true;
			node.style.transition = "none";
		}
		// Resistance above the resting position rather than a gap under it.
		dy = delta > 0 ? delta : delta / 4;
		node.style.transform = `translate3d(0, ${Math.max(0, dy)}px, 0)`;
	};

	const onDown = (e: PointerEvent) => {
		// The same component can be a sheet on touch and a side drawer on a pointer
		// layout; the drag belongs only to the sheet.
		if (disabled || id !== null || e.button !== 0) return;
		const scroller = node.querySelector<HTMLElement>("[data-sheet-scroll]");
		if (scroller?.contains(e.target as Node) && scroller.scrollTop > 0) return;
		id = e.pointerId;
		startY = e.clientY;
		startX = e.clientX;
		startedAt = e.timeStamp;
		dy = 0;
		touchDriven = false;
	};
	const onMove = (e: PointerEvent) => {
		if (e.pointerId !== id || touchDriven) return;
		const delta = e.clientY - startY;
		if (!dragging) {
			if (delta < SLOP) return;
			// Capture keeps the gesture alive if the finger leaves the sheet; it
			// can legitimately fail, and the drag still works without it.
			try {
				node.setPointerCapture(e.pointerId);
			} catch {}
		}
		drive(delta);
	};
	const onTouchMove = (e: TouchEvent) => {
		// `id === null` means onDown already refused this gesture — a list scrolled
		// off its top keeps it.
		if (disabled || id === null) return;
		const t = e.touches[0];
		if (!t) return;
		const delta = t.clientY - startY;
		// Downward only, and not while the finger is mostly travelling sideways:
		// the sort and status strips inside these sheets scroll horizontally.
		if (!dragging && (delta < SLOP || delta <= Math.abs(t.clientX - startX)))
			return;
		if (e.cancelable) e.preventDefault();
		touchDriven = true;
		drive(delta);
	};
	const onUp = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		const velocity = dy / Math.max(1, e.timeStamp - startedAt);
		const far = dy > (node.offsetHeight || 0) * DISMISS_RATIO;
		id = null;
		if (!dragging) return;
		dragging = false;
		if (far || velocity > FLICK) {
			reset(false); // hand the exit to the fly transition
			onDismiss();
		} else {
			reset(true);
		}
	};
	const onCancel = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		id = null;
		dragging = false;
		touchDriven = false;
		reset(true);
	};

	node.addEventListener("pointerdown", onDown);
	node.addEventListener("pointermove", onMove);
	node.addEventListener("pointerup", onUp);
	node.addEventListener("pointercancel", onCancel);
	node.addEventListener("touchmove", onTouchMove, { passive: false });

	return {
		update(next: { onDismiss: () => void; disabled?: boolean }) {
			onDismiss = next.onDismiss;
			disabled = next.disabled ?? false;
			if (disabled && id !== null) {
				id = null;
				dragging = false;
				touchDriven = false;
				reset(false);
			}
		},
		destroy() {
			node.removeEventListener("pointerdown", onDown);
			node.removeEventListener("pointermove", onMove);
			node.removeEventListener("pointerup", onUp);
			node.removeEventListener("pointercancel", onCancel);
			node.removeEventListener("touchmove", onTouchMove);
		},
	};
}
