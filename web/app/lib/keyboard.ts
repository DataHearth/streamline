// Least the page shrinks by when the on-screen keyboard opens: less is the
// browser's own bars.
export const KEYBOARD_MIN_PX = 120;

// Widths remembered at once: a window dragged by its edge passes through one
// per pixel.
const WIDTHS_KEPT = 16;

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

// Whether focus on this element brings the on-screen keyboard up: a field
// that takes text, unless it is read-only or asks for no keyboard.
export const typesText = (el: EventTarget | null): boolean => {
	if (!(el instanceof HTMLElement) || el.inputMode === "none") return false;
	if (el instanceof HTMLTextAreaElement) return !el.readOnly;
	if (el instanceof HTMLInputElement)
		return !el.readOnly && !NOT_TYPED.has(el.type);
	return el.isContentEditable;
};

// One reading of the page: its size, whether a text field has focus, and what
// prompted it: the page resized, focus moved, or focus has been out of every
// field for a while (`idle`).
export type KeyboardReading = {
	width: number;
	height: number;
	typing: boolean;
	kind: "resize" | "focus" | "idle";
};

// What a reading tells: whether the keyboard is up, and the page's height
// without it, if known.
export type KeyboardState = { up: boolean; page: number | undefined };

const remember = (map: Map<number, number>, width: number, value: number) => {
	map.delete(width);
	map.set(width, value);
	if (map.size > WIDTHS_KEPT) map.delete(map.keys().next().value as number);
};

// Tells the on-screen keyboard from the page's size, and the page's height
// without it.
//
// The keyboard is up once a text field has focus and the page stands at least
// KEYBOARD_MIN_PX shorter than its height without the keyboard, and it stays up
// until the page grows back: focus leaving the field (a tap on a button, on
// nothing) does not close it, the page growing does, so nothing comes back
// above a keyboard on its way down. Focus out of every field for a while with
// the page not grown back (`idle`) means the shrink was no keyboard (a split
// screen made while typing): the page's height is the one it has, and the
// keyboard is down.
//
// The height without the keyboard is kept per width, so a rotation finds the
// one the new width had, and only a resize moves it (the first reading counts
// as one). With a field focused it can only grow, and so it can while the
// keyboard is up (focus on a button beside a keyboard that grows a suggestion
// strip). Otherwise the page shrinking is the page's own (a split screen, the
// browser's bars) and lowers it, while the page growing is the keyboard
// closing and raises it.
//
// A width met while the keyboard is up (a rotation to a side never seen) has
// nothing to count from: the keyboard stays up through focus moving between
// fields, and the height stays unknown until the page grows there by
// KEYBOARD_MIN_PX, the keyboard closing, or focus goes idle.
//
// The player (streamline-player, src/lib/platform/keyboard.ts) runs the same
// rules; a change here goes there too.
export function keyboardWatch(): (reading: KeyboardReading) => KeyboardState {
	const pageAt = new Map<number, number>();
	const lastAt = new Map<number, number>();
	const lowAt = new Map<number, number>();
	let up = false;
	const settle = (width: number, height: number): KeyboardState => {
		remember(pageAt, width, height);
		remember(lastAt, width, height);
		lowAt.delete(width);
		up = false;
		return { up, page: height };
	};
	return ({ width, height, typing, kind }) => {
		const known = pageAt.get(width);
		if (known === undefined) {
			if (up && kind !== "idle") {
				const low = Math.min(lowAt.get(width) ?? height, height);
				remember(lowAt, width, low);
				if (height - low < KEYBOARD_MIN_PX) return { up, page: undefined };
				return settle(width, height);
			}
			if (kind === "focus") return { up, page: undefined };
			return settle(width, height);
		}
		if (kind === "idle" && !typing) return settle(width, height);
		let page = known;
		if (kind === "resize") {
			const last = lastAt.get(width) ?? height;
			page = !typing && !up && height < last ? height : Math.max(known, height);
			remember(lastAt, width, height);
		}
		remember(pageAt, width, page);
		up = page - height >= KEYBOARD_MIN_PX && (typing || up);
		return { up, page };
	};
}
