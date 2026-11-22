import { createSubscriber } from "svelte/reactivity";

// Whether the on-screen keyboard is up, for the phone shell's fixed bottom
// layers (BottomNav, the add pill), which index.html's
// `interactive-widget=resizes-content` would otherwise lift above it.
//
// No browser says so directly. On a touch screen, the keyboard reads as up
// while a text field has focus and the visual viewport stands at least
// KEYBOARD_MIN_PX shorter than it did before; anything less is the browser's
// own bars. Without focus the height is the page's own (a split screen shrinks
// it too). The visual viewport is what the keyboard shrinks everywhere, iOS
// included; both sides are taken times its scale, so a pinch, or iOS zooming
// in on a focused field, is neither a keyboard nor a rotation.

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
// The page's height without the keyboard, per width: a rotation with the
// keyboard up finds the height it had at that width before.
const fullAt = new Map<number, number>();

function measure(): boolean {
	// Only a touch screen brings an on-screen keyboard up. With a mouse alone, a
	// field keeping focus while the window is made shorter is just that.
	if (!window.matchMedia("(any-pointer: coarse)").matches) return false;
	const vv = window.visualViewport;
	const width = Math.round(vv ? vv.width * vv.scale : window.innerWidth);
	const height = vv ? vv.height * vv.scale : window.innerHeight;
	const typing = typesText(document.activeElement);
	const full = typing ? Math.max(fullAt.get(width) ?? height, height) : height;
	fullAt.set(width, full);
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
