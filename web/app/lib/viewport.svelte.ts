import { onMount } from "svelte";

// createMediaQuery returns a reactive getter for a media query. Every width
// breakpoint a component switches on is read through it, so they all see a
// resize the same way.
//
// Returns false during SSR and until mount. Every caller uses it to decide
// between a touch surface and a desktop one, and the desktop layout is the one
// that must not flash on a phone — so the touch branch is the false-y default.
//
// A page in the background, or just back from it, can be told of a size it does
// not keep: a phone that reads past md for a moment swaps its shell and its
// sheets for the desktop ones and back, and an open sheet moves under the finger
// coming back to it. So a change is read only while the page shows, and on its
// return only once the size has had SETTLE_MS to settle.
const SETTLE_MS = 250;

export function createMediaQuery(query: string): () => boolean {
	let matches = $state(false);
	onMount(() => {
		const mql = window.matchMedia(query);
		let settling: ReturnType<typeof setTimeout> | undefined;
		const read = () => (matches = mql.matches);
		const sync = () => {
			if (document.visibilityState === "visible" && settling === undefined) read();
		};
		const back = () => {
			clearTimeout(settling);
			settling = undefined;
			if (document.visibilityState !== "visible") return;
			settling = setTimeout(() => {
				settling = undefined;
				read();
			}, SETTLE_MS);
		};
		read();
		mql.addEventListener("change", sync);
		document.addEventListener("visibilitychange", back);
		return () => {
			clearTimeout(settling);
			mql.removeEventListener("change", sync);
			document.removeEventListener("visibilitychange", back);
		};
	});
	return () => matches;
}

// Below md the shell is the phone one: bottom navigation, sheets, the search page.
export const BELOW_MD = "(max-width: 767px)";
export const FROM_MD = "(min-width: 768px)";
export const FROM_LG = "(min-width: 1024px)";

// The settings shell switches at lg, not md. Below it there is one settings
// navigation (the index list) and the sub-sidebar is gone; see SettingsSidebar.
export const SETTINGS_DESKTOP = FROM_LG;

export function createSettingsDesktop(): () => boolean {
	return createMediaQuery(SETTINGS_DESKTOP);
}
