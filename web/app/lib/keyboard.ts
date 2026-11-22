// Least the page shrinks by when the on-screen keyboard opens: less is the
// browser's own bars.
export const KEYBOARD_MIN_PX = 120;

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

// Tells the on-screen keyboard from the page's size: it is up while a text
// field has focus and the page stands at least KEYBOARD_MIN_PX shorter than it
// does at the same width without it.
//
// That height is kept per width, so a rotation with the keyboard up finds the
// one the new width had. Only a resize moves it (the first reading counts as
// one): with no field focused it is the page's own (a split screen lowers it
// too), with one it can only grow. A change of focus moves nothing, so a tap
// on a button while the keyboard is still up never takes its height for the
// page's. A width first met with a field focused (a rotation to a side never
// seen) has nothing to count from: the keyboard keeps the state it had until
// the page grows by KEYBOARD_MIN_PX there, which is the keyboard closing.
//
// The player (streamline-player, src/lib/ui/keyboard.ts) runs the same rules;
// a change here goes there too.
export function keyboardWatch(): (reading: KeyboardReading) => boolean {
	const fullAt = new Map<number, number>();
	const lowAt = new Map<number, number>();
	let up = false;
	return ({ width, height, typing, resized }) => {
		const full = fullAt.get(width);
		if (full === undefined) {
			if (typing) {
				const low = Math.min(lowAt.get(width) ?? height, height);
				lowAt.set(width, low);
				if (height - low < KEYBOARD_MIN_PX) return up;
			}
			if (resized || typing) {
				fullAt.set(width, height);
				lowAt.delete(width);
			}
			up = false;
			return up;
		}
		const page = resized ? (typing ? Math.max(full, height) : height) : full;
		fullAt.set(width, page);
		up = typing && page - height >= KEYBOARD_MIN_PX;
		return up;
	};
}
