// Drag a bottom sheet down to dismiss it. Shared by the library filter sheet
// and the bulk-action sheet.
//
// Transform writes go straight to the node: animating anything that relayouts
// costs a frame per pointermove on mobile Gecko, which is the whole reason the
// add sheet moved to a pure-transform drag. The gesture only starts from the
// header or from a list already scrolled to the top — otherwise it belongs to
// the list.

const DISMISS_RATIO = 0.3;
const FLICK = 0.5; // px per ms
const SLOP = 6; // a tap on a row must stay a tap

export function sheetSwipe(
	node: HTMLElement,
	params: { onDismiss: () => void },
) {
	let onDismiss = params.onDismiss;
	let id: number | null = null;
	let startY = 0;
	let startedAt = 0;
	let dy = 0;
	let dragging = false;

	const reset = (animate: boolean) => {
		node.style.transition = animate
			? "transform var(--dur-base, 200ms) var(--ease, ease-out)"
			: "";
		node.style.transform = "";
	};

	const onDown = (e: PointerEvent) => {
		if (id !== null || e.button !== 0) return;
		const scroller = node.querySelector<HTMLElement>("[data-sheet-scroll]");
		if (scroller?.contains(e.target as Node) && scroller.scrollTop > 0) return;
		id = e.pointerId;
		startY = e.clientY;
		startedAt = e.timeStamp;
		dy = 0;
	};
	const onMove = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		const delta = e.clientY - startY;
		if (!dragging) {
			if (delta < SLOP) return;
			dragging = true;
			node.style.transition = "none";
			// Capture keeps the gesture alive if the finger leaves the sheet; it
			// can legitimately fail, and the drag still works without it.
			try {
				node.setPointerCapture(e.pointerId);
			} catch {}
		}
		// Resistance above the resting position rather than a gap under it.
		dy = delta > 0 ? delta : delta / 4;
		node.style.transform = `translate3d(0, ${Math.max(0, dy)}px, 0)`;
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
		reset(true);
	};

	node.addEventListener("pointerdown", onDown);
	node.addEventListener("pointermove", onMove);
	node.addEventListener("pointerup", onUp);
	node.addEventListener("pointercancel", onCancel);

	return {
		update(next: { onDismiss: () => void }) {
			onDismiss = next.onDismiss;
		},
		destroy() {
			node.removeEventListener("pointerdown", onDown);
			node.removeEventListener("pointermove", onMove);
			node.removeEventListener("pointerup", onUp);
			node.removeEventListener("pointercancel", onCancel);
		},
	};
}
