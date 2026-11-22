import { keyboardWatch, typesText } from "@lib/keyboard";
import { createSubscriber } from "svelte/reactivity";

// Whether the on-screen keyboard is up, for the phone shell's fixed bottom
// layers (BottomNav, the add pill), which index.html's
// `interactive-widget=resizes-content` would otherwise lift above it. No
// browser says so directly: lib/keyboard.ts tells it from the page's size.

const watch = keyboardWatch();
let up = false;

// Only a touch screen brings an on-screen keyboard up: with a mouse alone, a
// field keeping focus while the window is made shorter is just that. Both
// sides come from the visual viewport, which the keyboard shrinks everywhere
// (on iOS it is the only one), times its scale, so a pinch, or iOS zooming in
// on a focused field, is neither a keyboard nor a rotation.
function measure(): boolean {
	if (!window.matchMedia("(any-pointer: coarse)").matches) return false;
	const vv = window.visualViewport;
	const width = Math.round(vv ? vv.width * vv.scale : window.innerWidth);
	const height = vv ? vv.height * vv.scale : window.innerHeight;
	return watch(width, height, typesText(document.activeElement));
}

const subscribe = createSubscriber((update) => {
	const vv = window.visualViewport;
	let frame = 0;
	const set = (next: boolean) => {
		if (next === up) return;
		up = next;
		update();
	};
	// A resize is read at once, before anything that lays out by the page's
	// size reads it. A blur waits a frame: moving from one field to the next
	// blurs the first before it focuses the second.
	const now = () => {
		cancelAnimationFrame(frame);
		set(measure());
	};
	const later = () => {
		cancelAnimationFrame(frame);
		frame = requestAnimationFrame(() => set(measure()));
	};
	// Read on subscribing too: with no reader left the listeners stop, and the
	// value would be whatever they last saw.
	up = measure();
	vv?.addEventListener("resize", now);
	window.addEventListener("resize", now);
	document.addEventListener("focusin", now);
	document.addEventListener("focusout", later);
	return () => {
		cancelAnimationFrame(frame);
		vv?.removeEventListener("resize", now);
		window.removeEventListener("resize", now);
		document.removeEventListener("focusin", now);
		document.removeEventListener("focusout", later);
	};
});

export function keyboardUp(): boolean {
	subscribe();
	return up;
}
