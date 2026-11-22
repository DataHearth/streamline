import { keyboardWatch, typesText } from "@lib/keyboard";
import { createSubscriber } from "svelte/reactivity";

// Whether the on-screen keyboard is up, for the phone shell's fixed bottom
// layers (BottomNav, the add pill, #main's clearance for them), which
// index.html's `interactive-widget=resizes-content` would otherwise lift above
// it. No browser says so directly: lib/keyboard.ts tells it from the page's
// size.

const watch = keyboardWatch();
let up = false;

// Only a touch screen brings an on-screen keyboard up, and the app's touch
// screen is the primary pointer's (`pointer-fine` in docs/agents/frontend.md):
// with a mouse or a trackpad, a field keeping focus while the window is made
// shorter is just that. Both sides come from the visual viewport, which the
// keyboard shrinks everywhere (on iOS it is the only one), times its scale, so
// a pinch, or iOS zooming in on a focused field, is neither a keyboard nor a
// rotation.
function measure(resized: boolean): boolean {
	if (!window.matchMedia("(pointer: coarse)").matches) return false;
	const vv = window.visualViewport;
	return watch({
		width: Math.round(vv ? vv.width * vv.scale : window.innerWidth),
		height: vv ? vv.height * vv.scale : window.innerHeight,
		typing: typesText(document.activeElement),
		resized,
	});
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
	// size reads it, and so is focus reaching a text field. Focus leaving one
	// waits a frame, whether it leaves for nothing or for a button: moving to
	// another field, or a Select focusing its own search field, passes through
	// both.
	const resized = () => {
		cancelAnimationFrame(frame);
		set(measure(true));
	};
	const later = () => {
		cancelAnimationFrame(frame);
		frame = requestAnimationFrame(() => set(measure(false)));
	};
	const focused = (event: FocusEvent) => {
		if (!typesText(event.target)) return later();
		cancelAnimationFrame(frame);
		set(measure(false));
	};
	// Read on subscribing too, as a fresh size: with no reader left the
	// listeners stop, and the value would be whatever they last saw.
	up = measure(true);
	vv?.addEventListener("resize", resized);
	window.addEventListener("resize", resized);
	document.addEventListener("focusin", focused);
	document.addEventListener("focusout", later);
	return () => {
		cancelAnimationFrame(frame);
		vv?.removeEventListener("resize", resized);
		window.removeEventListener("resize", resized);
		document.removeEventListener("focusin", focused);
		document.removeEventListener("focusout", later);
	};
});

export function keyboardUp(): boolean {
	subscribe();
	return up;
}
