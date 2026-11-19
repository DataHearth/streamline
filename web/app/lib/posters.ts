export type PosterRef = { id: number };

export function posterUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/poster.jpg`;
}

export function tvPosterUrl(id: number): string {
	return `/posters/tvshows/${id}/poster.jpg`;
}

export function albumPosterUrl(id: number): string {
	return `/posters/albums/${id}/poster.jpg`;
}

export function artistPosterUrl(id: number): string {
	return `/posters/artists/${id}/poster.jpg`;
}

// The one proxy for art of a title that is not in the library yet: the key is
// an artist or release-group MBID, or a Hardcover book id.
export function lookupPosterUrl(kind: "artists" | "albums" | "books", key: string | number): string {
	return `/posters/lookup/${kind}/${key}/poster.jpg`;
}

export function authorPosterUrl(id: number): string {
	return `/posters/authors/${id}/poster.jpg`;
}

export function bookPosterUrl(id: number): string {
	return `/posters/books/${id}/poster.jpg`;
}

export function personPhotoUrl(id: number): string {
	return `/posters/people/${id}/poster.jpg`;
}

// The widths the poster proxy scales a library poster to (`variantWidths` in
// internal/posters/variant.go holds the same list).
const POSTER_WIDTHS = [120, 200, 320, 480, 640];

// The width the full-size poster is advertised at: past the widest rung's.
const ORIGINAL_WIDTH = 400;

// The `sizes` a lazily loaded poster gets: the browser reads the slot from
// layout, and one that does not know `auto` assumes an ordinary card.
export const POSTER_SIZES = "auto, 180px";

// A `srcset` over the proxy's widths, or undefined for art the proxy does not
// scale. Each candidate is advertised at half its real width, so the browser
// picks the file holding about two pixels per device pixel of the slot: Chrome
// draws a poster reduced four or five times in one pass aliased, and one
// served at the slot's own size soft.
export function posterSrcset(src: string, query = ""): string | undefined {
	if (!src.startsWith("/posters/") || src.startsWith("/posters/lookup/")) return undefined;
	const sep = query ? "&" : "?";
	return [
		...POSTER_WIDTHS.map((w) => `${src}${query}${sep}w=${w} ${w / 2}w`),
		`${src}${query} ${ORIGINAL_WIDTH}w`,
	].join(", ");
}

// Session-scoped negative cache. Poster URLs are constructed client-side, so
// nothing tells the SPA which media has no artwork — without this, every card
// remount replays the full retry ladder against a poster that will keep
// 404ing. A full page reload clears it, picking up posters fetched since.
const missing = new Set<string>();

export function isPosterMissing(src: string): boolean {
	return missing.has(src);
}

export function markPosterMissing(src: string) {
	missing.add(src);
}
