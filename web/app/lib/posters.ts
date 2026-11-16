// tmdb_id is the art's version: the cache is keyed by the row id and served
// as fresh for a day, and a re-identify changes the art under that id. A
// caller holding the match passes it, so the URL moves with the match and the
// browser cannot answer the new title from the old one's cached copy.
export type PosterRef = { id: number; tmdb_id?: number };

function artVersion(movie: PosterRef): string {
	return movie.tmdb_id ? `?v=${movie.tmdb_id}` : "";
}

export function posterUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/poster.jpg${artVersion(movie)}`;
}

// A 404 means no logo is cached; has_logo on the movie says so up front.
export function movieLogoUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/logo.png${artVersion(movie)}`;
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
