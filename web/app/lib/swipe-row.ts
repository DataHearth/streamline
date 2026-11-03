// Swipe a touch row sideways to act on it: right for the row's start action,
// left for its end action. The row follows the finger, and letting go past the
// threshold commits; anything short of it springs back. Touch and pen only — a
// mouse has the table's buttons.
//
// The row keeps `touch-action: pan-y`, so the browser still owns vertical
// scrolling and only a gesture that is clearly horizontal becomes a swipe; a
// diagonal start goes to the scroller. Moving past the long press's slop also
// cancels that hold, so the two gestures never both fire.

import { swallowNextClick } from "./long-press";

export type SwipeSide = "start" | "end";

export type SwipeRowOptions = {
	// Which directions have an action. A side without one only gives a little,
	// so the row still answers the finger without suggesting there is more.
	start: boolean;
	end: boolean;
	disabled?: boolean;
	// Every frame of the drag, and once more with 0 when it settles.
	onOffset: (dx: number, settling: boolean) => void;
	// Fires once each time the drag crosses the threshold, either way, so the
	// revealed action can say whether letting go will run it.
	onArmed?: (armed: boolean) => void;
	onCommit: (side: SwipeSide) => void;
};

const LOCK = 10; // px of travel before a direction is decided
const RESIST = 0.2; // how far a side with no action follows the finger
const RESIST_MAX = 28;

export function swipeRow(node: HTMLElement, initial: SwipeRowOptions) {
	let opts = initial;
	let id: number | null = null;
	let x0 = 0;
	let y0 = 0;
	let state: "idle" | "pending" | "dragging" = "idle";
	let dx = 0;
	let armed = false;

	node.style.touchAction = "pan-y";

	// A third of the row, capped so a wide phone in landscape doesn't need a
	// half-screen fling.
	const threshold = () => Math.min(112, node.offsetWidth * 0.3);

	function reset() {
		id = null;
		state = "idle";
		if (armed) opts.onArmed?.(false);
		armed = false;
		if (dx !== 0) {
			dx = 0;
			opts.onOffset(0, true);
		}
	}

	function down(e: PointerEvent) {
		if (opts.disabled || e.pointerType === "mouse" || id !== null) return;
		if (!opts.start && !opts.end) return;
		id = e.pointerId;
		x0 = e.clientX;
		y0 = e.clientY;
		state = "pending";
	}

	function move(e: PointerEvent) {
		if (e.pointerId !== id) return;
		const mx = e.clientX - x0;
		const my = e.clientY - y0;
		if (state === "pending") {
			if (Math.abs(my) > LOCK && Math.abs(my) >= Math.abs(mx)) {
				// A scroll. Let it go for the rest of this gesture.
				id = null;
				state = "idle";
				return;
			}
			if (Math.abs(mx) <= LOCK || Math.abs(mx) < Math.abs(my) * 1.2) return;
			state = "dragging";
			node.setPointerCapture?.(e.pointerId);
		}
		if (state !== "dragging") return;
		const allowed = mx > 0 ? opts.start : opts.end;
		const raw = mx - Math.sign(mx) * LOCK;
		dx = allowed
			? Math.max(-node.offsetWidth * 0.85, Math.min(node.offsetWidth * 0.85, raw))
			: Math.sign(raw) * Math.min(RESIST_MAX, Math.abs(raw) * RESIST);
		const nowArmed = allowed && Math.abs(dx) >= threshold();
		if (nowArmed !== armed) {
			armed = nowArmed;
			if (armed) navigator.vibrate?.(8);
			opts.onArmed?.(armed);
		}
		opts.onOffset(dx, false);
	}

	function up(e: PointerEvent) {
		if (e.pointerId !== id) return;
		const wasDragging = state === "dragging";
		const commit = wasDragging && armed ? (dx > 0 ? "start" : "end") : null;
		reset();
		if (wasDragging) swallowNextClick();
		if (commit) opts.onCommit(commit);
	}

	function cancel(e: PointerEvent) {
		if (e.pointerId === id) reset();
	}

	node.addEventListener("pointerdown", down);
	node.addEventListener("pointermove", move);
	node.addEventListener("pointerup", up);
	node.addEventListener("pointercancel", cancel);

	return {
		update(next: SwipeRowOptions) {
			opts = next;
			if (opts.disabled && state !== "idle") reset();
		},
		destroy() {
			node.removeEventListener("pointerdown", down);
			node.removeEventListener("pointermove", move);
			node.removeEventListener("pointerup", up);
			node.removeEventListener("pointercancel", cancel);
		},
	};
}
