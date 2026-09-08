// Small, best-effort view preferences (sort order and the like). localStorage
// throws outright when the browser blocks storage — Safari private mode, some
// embedded webviews — and a lost preference must never take the page with it.

export function loadPref(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

export function savePref(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		// Preference is cosmetic; the page works fine without it persisting.
	}
}

// The library lists stamp their current filter/sort query string here so a
// detail page's back link returns to the library as it was left. The list URL
// itself is not reachable from the detail page — Routify navigates by pushState
// and the browser's own back entry is only right when the detail page was
// opened from the list.
export const MOVIES_SEARCH = "streamline:movies:search";
export const SERIES_SEARCH = "streamline:series:search";

export function listHref(path: string, key: string) {
	const search = loadPref(key);
	return search ? `${path}?${search}` : path;
}
