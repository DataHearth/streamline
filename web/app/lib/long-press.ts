// Press and hold on a touch row: the phone way into a selection, the same
// gesture PosterCard runs on the library grids. Touch and pen only — a mouse
// has the table's checkboxes. 480ms with 8px of slop, so a scroll that starts
// on a row is still a scroll.

export type LongPressOptions = {
	onLongPress: () => void;
	disabled?: boolean;
};

const HOLD_MS = 480;
const HOLD_SLOP = 8;

export function longPress(node: HTMLElement, initial: LongPressOptions) {
	let opts = initial;
	let timer: number | null = null;
	let x = 0;
	let y = 0;
	let held = false;

	function cancel() {
		if (timer !== null) {
			clearTimeout(timer);
			timer = null;
		}
	}
	function down(e: PointerEvent) {
		if (opts.disabled || e.pointerType === "mouse") return;
		cancel();
		x = e.clientX;
		y = e.clientY;
		held = false;
		timer = window.setTimeout(() => {
			timer = null;
			held = true;
			swallowNextClick();
			navigator.vibrate?.(10);
			opts.onLongPress();
		}, HOLD_MS);
	}
	function move(e: PointerEvent) {
		if (timer === null) return;
		if (Math.abs(e.clientX - x) > HOLD_SLOP || Math.abs(e.clientY - y) > HOLD_SLOP)
			cancel();
	}
	function contextMenu(e: MouseEvent) {
		// Android fires the context menu on the same gesture.
		if (timer !== null || held) e.preventDefault();
	}

	node.addEventListener("pointerdown", down);
	node.addEventListener("pointermove", move);
	node.addEventListener("pointerup", cancel);
	node.addEventListener("pointercancel", cancel);
	node.addEventListener("pointerleave", cancel);
	node.addEventListener("contextmenu", contextMenu);

	return {
		update(next: LongPressOptions) {
			opts = next;
			if (opts.disabled) cancel();
		},
		destroy() {
			cancel();
			node.removeEventListener("pointerdown", down);
			node.removeEventListener("pointermove", move);
			node.removeEventListener("pointerup", cancel);
			node.removeEventListener("pointercancel", cancel);
			node.removeEventListener("pointerleave", cancel);
			node.removeEventListener("contextmenu", contextMenu);
		},
	};
}

// The click the browser fires when the finger lifts would immediately toggle
// the row the hold just selected. Swallowed on the window, in capture, rather
// than on the node: entering selection mode usually re-renders the row, so the
// element that started the hold may not be the one the click lands on.
function swallowNextClick() {
	const swallow = (e: MouseEvent) => {
		e.preventDefault();
		e.stopPropagation();
		disarm();
	};
	// A lift the browser turns into no click at all must not eat the next tap,
	// so the next gesture's own pointerdown disarms it first.
	const disarm = () => {
		window.removeEventListener("click", swallow, true);
		window.removeEventListener("pointerdown", disarm, true);
	};
	window.addEventListener("click", swallow, true);
	window.addEventListener("pointerdown", disarm, true);
}
