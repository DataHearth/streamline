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

// Whether focus on this element brings the on-screen keyboard up.
export const typesText = (el: EventTarget | null): boolean =>
	el instanceof HTMLTextAreaElement ||
	(el instanceof HTMLInputElement && !NOT_TYPED.has(el.type)) ||
	(el instanceof HTMLElement && el.isContentEditable);

// One reading of the page: its size, whether a text field has focus, and
// whether it was resized.
export type KeyboardReading = {
	width: number;
	height: number;
	typing: boolean;
	resized: boolean;
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
// above a keyboard on its way down.
//
// The height without the keyboard is kept per width, so a rotation finds the
// one the new width had, and only a resize moves it (the first reading counts
// as one). With a field focused it can only grow. Without one, the page
// shrinking is the page's own (a split screen, the browser's bars) and lowers
// it, while the page growing is the keyboard closing and raises it.
//
// A width first met with the keyboard up (a rotation to a side never seen) has
// nothing to count from: the keyboard stays up and the height stays unknown
// until the page grows there by KEYBOARD_MIN_PX, which is the keyboard closing.
//
// The player (streamline-player, src/lib/platform/keyboard.ts) runs the same
// rules; a change here goes there too.
export function keyboardWatch(): (reading: KeyboardReading) => KeyboardState {
	const pageAt = new Map<number, number>();
	const lastAt = new Map<number, number>();
	const lowAt = new Map<number, number>();
	let up = false;
	return ({ width, height, typing, resized }) => {
		const known = pageAt.get(width);
		if (known === undefined) {
			if (up && typing) {
				const low = Math.min(lowAt.get(width) ?? height, height);
				remember(lowAt, width, low);
				if (height - low < KEYBOARD_MIN_PX) return { up, page: undefined };
			} else if (!resized) {
				up = false;
				return { up, page: undefined };
			}
			remember(pageAt, width, height);
			remember(lastAt, width, height);
			lowAt.delete(width);
			up = false;
			return { up, page: height };
		}
		let page = known;
		if (resized) {
			const last = lastAt.get(width) ?? height;
			page = !typing && height < last ? height : Math.max(known, height);
			remember(pageAt, width, page);
			remember(lastAt, width, height);
		}
		up = page - height >= KEYBOARD_MIN_PX && (typing || up);
		return { up, page };
	};
}
