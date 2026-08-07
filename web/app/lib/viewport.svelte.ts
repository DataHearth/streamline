import { onMount } from "svelte";

// createMediaQuery returns a reactive getter for a media query. Several routes
// hand-rolled this same onMount/matchMedia/addEventListener block; the settings
// rework needs it at a second breakpoint (lg, not md) so it moved here rather
// than being pasted a seventh time.
//
// Returns false during SSR and until mount. Every caller uses it to decide
// between a touch surface and a desktop one, and the desktop layout is the one
// that must not flash on a phone — so the touch branch is the false-y default.
export function createMediaQuery(query: string): () => boolean {
	let matches = $state(false);
	onMount(() => {
		const mql = window.matchMedia(query);
		const sync = () => (matches = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});
	return () => matches;
}

// The settings shell switches at lg, not md. Below it there is one settings
// navigation (the index list) and the sub-sidebar is gone; see SettingsSidebar.
export const SETTINGS_DESKTOP = "(min-width: 1024px)";

export function createSettingsDesktop(): () => boolean {
	return createMediaQuery(SETTINGS_DESKTOP);
}
