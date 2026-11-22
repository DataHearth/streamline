import {
	type KeyboardReading,
	keyboardWatch,
	typesText,
} from "@lib/keyboard";
import { createSubscriber } from "svelte/reactivity";

// Whether the on-screen keyboard is up, for the phone shell's fixed bottom
// layers (BottomNav, the add pill, the bulk bars, #main's clearance for them),
// which the page shrinking above the keyboard (index.html's
// `interactive-widget=resizes-content`, `--keyboard-inset` where it does not
// apply) would otherwise lift above it. No browser says so directly:
// lib/keyboard.ts tells it from the page's size.

const watch = keyboardWatch();
let up = false;

// How long focus stays out of every field, the page not grown back, before
// that means no keyboard.
const IDLE_MS = 1000;

// Only a touch screen brings an on-screen keyboard up, read as a coarse
// primary pointer, as the player reads it: with a mouse or a trackpad, a field
// keeping focus while the window is made shorter is just that. The page's size
// is read either way, so a pointer that turns coarse (a tablet mode) finds it
// known. Both sides come from the visual viewport, which the keyboard shrinks
// everywhere (on iOS it is the only one), times its scale, so a pinch, or iOS
// zooming in on a focused field, is neither a keyboard nor a rotation.
const COARSE = "(pointer: coarse)";

function measure(kind: KeyboardReading["kind"]): boolean {
	const vv = window.visualViewport;
	const touch = window.matchMedia(COARSE).matches;
	const reading = watch({
		width: Math.round(vv ? vv.width * vv.scale : window.innerWidth),
		height: vv ? vv.height * vv.scale : window.innerHeight,
		typing: touch && typesText(document.activeElement),
		kind,
	});
	return touch && reading.up;
}

// Where the layout viewport does not shrink for the keyboard (iOS Safari has
// no `interactive-widget`), the part of it the keyboard covers, for AppShell,
// the search page and the phone lookup screens to stop above. All three
// numbers are layout px (`innerHeight` follows the zoom in WebKit, the layout
// viewport's clientHeight does not), so iOS zooming in on a small field
// changes nothing. Elsewhere the layout viewport ends at the keyboard already,
// and it is 0.
function inset(keyboard: boolean) {
	const vv = window.visualViewport;
	const layout = document.documentElement.clientHeight;
	const px =
		keyboard && vv ? Math.max(0, layout - (vv.offsetTop + vv.height)) : 0;
	document.documentElement.style.setProperty(
		"--keyboard-inset",
		`${Math.round(px)}px`,
	);
}

// Read at once on every resize and focus change: a focus change never closes
// the keyboard (lib/keyboard.ts), so nothing waits for focus to settle. Focus
// leaving every field with the keyboard up starts the idle wait; focus
// reaching one ends it.
const subscribe = createSubscriber((update) => {
	const vv = window.visualViewport;
	const pointer = window.matchMedia(COARSE);
	let idle: ReturnType<typeof setTimeout> | undefined;
	const read = (kind: KeyboardReading["kind"]) => {
		const next = measure(kind);
		inset(next);
		if (next === up) return;
		up = next;
		update();
	};
	const resized = () => read("resize");
	// iOS moves the visual viewport over the page under the keyboard: only the
	// inset follows.
	const moved = () => inset(up);
	const focused = () => {
		clearTimeout(idle);
		read("focus");
		if (up && !typesText(document.activeElement)) {
			idle = setTimeout(() => read("idle"), IDLE_MS);
		}
	};
	// Read on subscribing too, as a fresh size: with no reader left the
	// listeners stop, and the value would be whatever they last saw.
	up = measure("resize");
	inset(up);
	vv?.addEventListener("resize", resized);
	vv?.addEventListener("scroll", moved);
	window.addEventListener("resize", resized);
	pointer.addEventListener("change", resized);
	document.addEventListener("focusin", focused);
	document.addEventListener("focusout", focused);
	return () => {
		clearTimeout(idle);
		document.documentElement.style.removeProperty("--keyboard-inset");
		vv?.removeEventListener("resize", resized);
		vv?.removeEventListener("scroll", moved);
		window.removeEventListener("resize", resized);
		pointer.removeEventListener("change", resized);
		document.removeEventListener("focusin", focused);
		document.removeEventListener("focusout", focused);
	};
});

export function keyboardUp(): boolean {
	subscribe();
	return up;
}
