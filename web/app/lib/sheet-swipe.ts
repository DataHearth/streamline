// Drag a bottom sheet down to dismiss it. Shared by every scrimmed bottom
// sheet (filters, bulk actions, detail and decision sheets).
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
// claimed in JS, the way LookupSheet claims its own. A drag the touch stream
// claimed is also ended by it: a late `pointercancel` does not drop the sheet
// under a finger that is still on it.
//
// The release decides from the finger's speed at the end, never from the
// average over the whole gesture: a pull that stops before letting go has no
// speed, and only how far it travelled counts. A fast flick wins in its own
// direction — a flick back up keeps the sheet even past the halfway mark. The
// average used to let a fast start lend a stopped release enough speed to
// dismiss.
//
// A dismissal slides the sheet off the rest of the way from where the finger
// left it and only then calls `onDismiss`, so the component's `fly` outro runs
// on a sheet already off screen. Handing the exit to that outro at release
// snapped the sheet back to its resting place first and then faded it out
// over 420px — the odd fade a stopped pull ended in. The scrim
// (`data-sheet-scrim`, a sibling of the sheet) fades with the travel, so it
// tracks the drag and is gone when the sheet is.
//
// The same physics as plein's ModalSheet (aslafy-z/plein), which this mirrors.

const DISMISS_RATIO = 0.5; // a stopped release past half the height dismisses
const FLICK = 0.45; // px per ms
const FLICK_WINDOW_MS = 100; // the release speed is measured over this trailing span
const FLICK_HOLD_MS = 150; // held still longer than this before release → no speed
const SLOP = 6; // a tap on a row must stay a tap
const CATCH_PX = 4; // further than this off its rest, the sheet is mid-flight
const SETTLE = "transform var(--dur-base, 200ms) var(--ease, ease-out)";
const SCRIM_SETTLE = "opacity var(--dur-base, 200ms) var(--ease, ease-out)";
const EXIT_MS = 280; // the sheets' own `fly` duration, cubicOut
const EXIT = `transform ${EXIT_MS}ms cubic-bezier(0.33, 1, 0.68, 1)`;
const EXIT_FALLBACK_MS = EXIT_MS + 170; // transitionend can fail to fire

type Sample = { y: number; t: number };

/** Downward speed (px/ms) at release: displacement over the trailing samples,
 *  on event timestamps. The span is the fixed window widened backwards across
 *  gaps that carry fast motion (moves a busy main thread delivered late and
 *  coalesced) and stops at a still gap — what precedes a pause must not lend
 *  the release any speed. A finger parked before letting go has none. */
function releaseVelocity(samples: Sample[], t: number): number {
	const last = samples[samples.length - 1];
	if (!last || t - last.t > FLICK_HOLD_MS) return 0;
	let i = samples.length - 1;
	while (i > 0) {
		const cur = samples[i]!;
		const prev = samples[i - 1]!;
		const gap = cur.t - prev.t;
		const fast = gap > 0 && Math.abs(cur.y - prev.y) / gap > FLICK / 2;
		if (last.t - prev.t > FLICK_WINDOW_MS && !fast) break;
		i--;
	}
	const from = samples[i]!;
	return last.t > from.t ? (last.y - from.y) / (last.t - from.t) : 0;
}

export function sheetSwipe(
	node: HTMLElement,
	params: { onDismiss: () => void; disabled?: boolean },
) {
	let onDismiss = params.onDismiss;
	let disabled = params.disabled ?? false;
	let id: number | null = null;
	let startY = 0;
	let startX = 0;
	// Where the sheet was when the finger landed: a grab mid spring-back holds
	// it there instead of snapping it to its rest first.
	let startTy = 0;
	let ty = 0;
	let h = 0;
	let samples: Sample[] = [];
	let dragging = false;
	let closing = false;
	let exitTimer = 0;
	let exitDone: (() => void) | null = null;
	// Both streams describe the same finger. Whichever claims the gesture first
	// drives it; the other stands down rather than painting the same frame twice.
	let touchDriven = false;

	const scrim = () =>
		node.parentElement?.querySelector<HTMLElement>(
			":scope > [data-sheet-scrim]",
		) ?? null;

	const currentTy = () => {
		const t = getComputedStyle(node).transform;
		if (!t || t === "none") return 0;
		try {
			return Math.max(0, new DOMMatrixReadOnly(t).m42);
		} catch {
			return 0;
		}
	};

	/** The one writer of the transform and of the scrim fade it implies. */
	const apply = (y: number) => {
		node.style.transform = y > 0 ? `translate3d(0, ${y}px, 0)` : "";
		const s = scrim();
		if (s) s.style.opacity = y > 0 && h > 0 ? String(Math.max(0, 1 - y / h)) : "";
	};

	const settle = () => {
		node.style.transition = SETTLE;
		const s = scrim();
		if (s) s.style.transition = SCRIM_SETTLE;
		apply(0);
	};

	const clearExit = () => {
		clearTimeout(exitTimer);
		if (exitDone) node.removeEventListener("transitionend", onExitEnd);
		exitDone = null;
	};
	const onExitEnd = (e: TransitionEvent) => {
		if (e.target === node && e.propertyName === "transform") exitDone?.();
	};

	const exit = () => {
		closing = true;
		node.style.transition = EXIT;
		const s = scrim();
		if (s) s.style.transition = `opacity ${EXIT_MS}ms linear`;
		const target = h || node.offsetHeight;
		// Already at the bottom: no transform changes, so no transitionend comes,
		// and the overlay would sit there invisible, swallowing taps.
		const travelled = target - ty < 1;
		apply(target);
		exitDone = () => {
			clearExit();
			onDismiss();
			// Svelte marks an outroing node inert. A sheet still live a frame later
			// was kept open by its owner, so it comes back rather than staying off
			// screen.
			requestAnimationFrame(() => {
				if (!node.isConnected || node.inert) return;
				closing = false;
				settle();
			});
		};
		node.addEventListener("transitionend", onExitEnd);
		exitTimer = window.setTimeout(
			() => exitDone?.(),
			travelled ? 0 : EXIT_FALLBACK_MS,
		);
	};

	const drive = (delta: number, t: number) => {
		if (!dragging) {
			dragging = true;
			h = node.offsetHeight;
			// The entry `fly` animates the transform over the inline one; a drag
			// starting during it takes over from where it had the sheet.
			for (const a of node.getAnimations?.() ?? []) a.finish();
			node.style.transition = "none";
			const s = scrim();
			if (s) {
				for (const a of s.getAnimations?.() ?? []) a.finish();
				s.style.transition = "none";
			}
		}
		const prev = samples[samples.length - 1];
		if (!prev || prev.t !== t) {
			samples.push({ y: startY + delta, t });
			while (
				samples.length > 1 &&
				t - samples[0]!.t > FLICK_WINDOW_MS + FLICK_HOLD_MS
			)
				samples.shift();
		}
		// Never above the resting position: no gap opens under the sheet.
		ty = Math.min(h, Math.max(0, startTy + delta));
		apply(ty);
	};

	const end = (t: number, cancelled: boolean) => {
		const wasDragging = dragging;
		id = null;
		dragging = false;
		touchDriven = false;
		if (!wasDragging) return;
		const v = cancelled ? 0 : releaseVelocity(samples, t);
		samples = [];
		const dismiss = Math.abs(v) > FLICK ? v > 0 : ty > h * DISMISS_RATIO;
		if (dismiss) exit();
		else settle();
	};

	const onDown = (e: PointerEvent) => {
		// The same component can be a sheet on touch and a side drawer on a pointer
		// layout; the drag belongs only to the sheet.
		if (disabled || closing || id !== null || e.button !== 0) return;
		const at = currentTy();
		// A sheet springing back is caught wherever it is pressed, the list
		// included; at rest, a list scrolled off its top keeps the gesture.
		if (at <= CATCH_PX) {
			const scroller = node.querySelector<HTMLElement>("[data-sheet-scroll]");
			if (scroller?.contains(e.target as Node) && scroller.scrollTop > 0)
				return;
		}
		id = e.pointerId;
		startY = e.clientY;
		startX = e.clientX;
		startTy = at;
		ty = at;
		samples = [{ y: e.clientY, t: e.timeStamp }];
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
		drive(delta, e.timeStamp);
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
		drive(delta, e.timeStamp);
	};
	const onUp = (e: PointerEvent) => {
		if (e.pointerId === id) end(e.timeStamp, false);
	};
	const onCancel = (e: PointerEvent) => {
		// A drag the touch stream owns ends with the touch, not with the browser
		// giving up on the pointer.
		if (e.pointerId !== id || touchDriven) return;
		end(e.timeStamp, true);
	};
	const onTouchEnd = (e: TouchEvent) => {
		if (touchDriven && id !== null) end(e.timeStamp, e.type === "touchcancel");
	};

	node.addEventListener("pointerdown", onDown);
	node.addEventListener("pointermove", onMove);
	node.addEventListener("pointerup", onUp);
	node.addEventListener("pointercancel", onCancel);
	node.addEventListener("touchmove", onTouchMove, { passive: false });
	node.addEventListener("touchend", onTouchEnd);
	node.addEventListener("touchcancel", onTouchEnd);

	return {
		update(next: { onDismiss: () => void; disabled?: boolean }) {
			onDismiss = next.onDismiss;
			disabled = next.disabled ?? false;
			if (disabled && id !== null) {
				id = null;
				dragging = false;
				touchDriven = false;
				samples = [];
				node.style.transition = "";
				apply(0);
			}
		},
		destroy() {
			clearExit();
			node.removeEventListener("pointerdown", onDown);
			node.removeEventListener("pointermove", onMove);
			node.removeEventListener("pointerup", onUp);
			node.removeEventListener("pointercancel", onCancel);
			node.removeEventListener("touchmove", onTouchMove);
			node.removeEventListener("touchend", onTouchEnd);
			node.removeEventListener("touchcancel", onTouchEnd);
		},
	};
}
