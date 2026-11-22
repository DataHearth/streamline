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
export const typesText = (el: Element | null): boolean =>
	el instanceof HTMLTextAreaElement ||
	(el instanceof HTMLInputElement && !NOT_TYPED.has(el.type)) ||
	(el instanceof HTMLElement && el.isContentEditable);

// Tells the on-screen keyboard from the page's size: it is up while a text
// field has focus and the page stands at least KEYBOARD_MIN_PX shorter than it
// did at the same width without it. Without focus the height is the page's
// own (a split screen shrinks it too). The height is kept per width, so a
// rotation with the keyboard up finds the one that width had before.
export function keyboardWatch(): (
	width: number,
	height: number,
	typing: boolean,
) => boolean {
	const fullAt = new Map<number, number>();
	return (width, height, typing) => {
		const full = typing ? Math.max(fullAt.get(width) ?? height, height) : height;
		fullAt.set(width, full);
		return typing && full - height >= KEYBOARD_MIN_PX;
	};
}
