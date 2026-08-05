// Drag a horizontally scrolling strip with the pointer.
//
// Touch already swipes these natively (and keeps its momentum), so this only
// claims MOUSE drags — a filter strip that scrolls on a phone but not under a
// cursor reads as broken on a tablet with a trackpad or in a device preview.
// A drag past the slop suppresses the click that would follow, otherwise a
// tap-to-filter would fire at the end of every drag.

const SLOP = 5;

export function dragScroll(node: HTMLElement) {
	let id: number | null = null;
	let startX = 0;
	let startScroll = 0;
	let dragged = false;

	const onDown = (e: PointerEvent) => {
		if (e.pointerType !== "mouse" || e.button !== 0) return;
		if (node.scrollWidth <= node.clientWidth) return;
		id = e.pointerId;
		startX = e.clientX;
		startScroll = node.scrollLeft;
		dragged = false;
	};
	const onMove = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		const dx = e.clientX - startX;
		if (!dragged) {
			if (Math.abs(dx) < SLOP) return;
			dragged = true;
			node.style.cursor = "grabbing";
			node.style.userSelect = "none";
			try {
				node.setPointerCapture(e.pointerId);
			} catch {}
		}
		node.scrollLeft = startScroll - dx;
	};
	const stop = (e: PointerEvent) => {
		if (e.pointerId !== id) return;
		id = null;
		node.style.cursor = "";
		node.style.userSelect = "";
	};
	// Capture phase: the strip's own buttons must not see the click that ends a
	// drag. `dragged` stays true until this runs, then resets for the next press.
	const onClick = (e: MouseEvent) => {
		if (!dragged) return;
		dragged = false;
		e.preventDefault();
		e.stopPropagation();
	};

	node.addEventListener("pointerdown", onDown);
	node.addEventListener("pointermove", onMove);
	node.addEventListener("pointerup", stop);
	node.addEventListener("pointercancel", stop);
	node.addEventListener("click", onClick, true);

	return {
		destroy() {
			node.removeEventListener("pointerdown", onDown);
			node.removeEventListener("pointermove", onMove);
			node.removeEventListener("pointerup", stop);
			node.removeEventListener("pointercancel", stop);
			node.removeEventListener("click", onClick, true);
		},
	};
}
