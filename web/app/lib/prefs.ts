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
