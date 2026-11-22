import { createSubscriber } from "svelte/reactivity";

// Whether the on-screen keyboard is up, for the phone shell's fixed bottom
// layers (BottomNav, the add pill), which index.html's
// `interactive-widget=resizes-content` would otherwise lift above it.
//
// No browser says so directly. On a touch screen, the keyboard reads as up
// while a text field has focus and the visual viewport stands at least
// KEYBOARD_MIN_PX shorter than it did before; anything less is the browser's
// own bars. Without focus the height is the page's own (a split screen shrinks
// it too), and a new width is a rotation, which starts over. The visual
// viewport is what the keyboard shrinks everywhere, iOS included, times its
// scale so a pinch is not a keyboard.

const KEYBOARD_MIN_PX = 120;

const NOT_TYPED = new Set([
	"button",
	"checkbox",
	"color",
	"file",
	"hidden",
	"image",
	"radio",
	"range",
	"reset",
	"submit",
]);

const typesText = (el: Element | null) =>
	el instanceof HTMLTextAreaElement ||
	(el instanceof HTMLInputElement && !NOT_TYPED.has(el.type)) ||
	(el instanceof HTMLElement && el.isContentEditable);

let up = false;
let width = 0;
let full = 0;

function measure(): boolean {
	// Only a touch screen brings an on-screen keyboard up. With a mouse alone, a
	// field keeping focus while the window is made shorter is just that.
	if (!window.matchMedia("(any-pointer: coarse)").matches) return false;
	const vv = window.visualViewport;
	const height = vv ? vv.height * vv.scale : window.innerHeight;
	const typing = typesText(document.activeElement);
	if (window.innerWidth !== width || !typing) {
		width = window.innerWidth;
		full = height;
	} else full = Math.max(full, height);
	return typing && full - height >= KEYBOARD_MIN_PX;
}

const subscribe = createSubscriber((update) => {
	const vv = window.visualViewport;
	let frame = 0;
	// A frame late: moving from one field to the next blurs the first before
	// focusing the second.
	const check = () => {
		cancelAnimationFrame(frame);
		frame = requestAnimationFrame(() => {
			const next = measure();
			if (next === up) return;
			up = next;
			update();
		});
	};
	check();
	vv?.addEventListener("resize", check);
	window.addEventListener("resize", check);
	document.addEventListener("focusin", check);
	document.addEventListener("focusout", check);
	return () => {
		cancelAnimationFrame(frame);
		vv?.removeEventListener("resize", check);
		window.removeEventListener("resize", check);
		document.removeEventListener("focusin", check);
		document.removeEventListener("focusout", check);
	};
});

export function keyboardUp(): boolean {
	subscribe();
	return up;
}
