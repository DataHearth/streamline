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
