// Horizontal swipe between months. Touch only — a pointer layout has the ‹ ›
// buttons and a mouse drag across a calendar means nothing.
//
// The gesture competes with the page's vertical scroll, so it does not claim
// itself on the first move: it waits until the travel is decisively sideways
// (RATIO) and past SLOP. Until then the browser keeps the touch and the page
// scrolls normally, which is why nothing here calls preventDefault.

const COMMIT = 56; // px of horizontal travel that counts as a swipe
const RATIO = 1.6; // |dx| must beat |dy| by this much to be horizontal
const SLOP = 10; // below this it is still a tap

export function monthSwipe(
	node: HTMLElement,
	params: { onPrev: () => void; onNext: () => void; disabled?: boolean },
) {
	let onPrev = params.onPrev;
	let onNext = params.onNext;
	let disabled = params.disabled ?? false;
	let id: number | null = null;
	let startX = 0;
	let startY = 0;
	let horizontal = false;
	let dead = false;

	const end = () => {
		id = null;
		horizontal = false;
		dead = false;
	};

	const onDown = (e: PointerEvent) => {
		if (disabled || id !== null || e.pointerType !== "touch") return;
		id = e.pointerId;
		startX = e.clientX;
		startY = e.clientY;
		horizontal = false;
		dead = false;
	};

	const onMove = (e: PointerEvent) => {
		if (e.pointerId !== id || dead || horizontal) return;
		const dx = Math.abs(e.clientX - startX);
		const dy = Math.abs(e.clientY - startY);
		if (dx < SLOP && dy < SLOP) return;
		// Whichever axis wins first owns the rest of the gesture. A drag that
		// starts vertical stays a scroll even if it curves sideways later.
		if (dx > dy * RATIO) horizontal = true;
		else dead = true;
	};

	const onUp = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		const dx = e.clientX - startX;
		const commit = horizontal && Math.abs(dx) >= COMMIT;
		end();
		if (!commit) return;
		// Content moves with the finger: dragging left reveals what comes next.
		if (dx < 0) onNext();
		else onPrev();
	};

	const onCancel = (e: PointerEvent) => {
		if (e.pointerId === id) end();
	};

	node.addEventListener("pointerdown", onDown, { passive: true });
	node.addEventListener("pointermove", onMove, { passive: true });
	node.addEventListener("pointerup", onUp, { passive: true });
	node.addEventListener("pointercancel", onCancel, { passive: true });

	return {
		update(next: {
			onPrev: () => void;
			onNext: () => void;
			disabled?: boolean;
		}) {
			onPrev = next.onPrev;
			onNext = next.onNext;
			disabled = next.disabled ?? false;
			if (disabled) end();
		},
		destroy() {
			node.removeEventListener("pointerdown", onDown);
			node.removeEventListener("pointermove", onMove);
			node.removeEventListener("pointerup", onUp);
			node.removeEventListener("pointercancel", onCancel);
		},
	};
}
