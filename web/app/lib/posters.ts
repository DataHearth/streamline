// A caller holding the row's provider id (tmdb_id, tvdb_id) passes it: the
// cache is keyed by the row id, a re-identify changes the images under that
// id, and an <img> on screen asks again only when its URL moves.
export type PosterRef = { id: number; tmdb_id?: number };

// The match each row had when this session first saw it. The URL carries the
// provider id only once it differs from that one, so every page shares one
// cached copy of an image until a re-identify changes it.
const firstMatch = new Map<string, number>();

function imageVersion(row: string, providerId?: number): string {
	if (!providerId) return "";
	const first = firstMatch.get(row);
	if (first === undefined) firstMatch.set(row, providerId);
	return first === undefined || first === providerId ? "" : `?v=${providerId}`;
}

export function moviePosterUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/poster.jpg${imageVersion(`movies/${movie.id}`, movie.tmdb_id)}`;
}

// A 404 means no logo is cached; has_logo on the movie says so up front.
export function movieLogoUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/logo.png${imageVersion(`movies/${movie.id}`, movie.tmdb_id)}`;
}

export function tvPosterUrl(id: number, tvdbId?: number): string {
	return `/posters/tvshows/${id}/poster.jpg${imageVersion(`tvshows/${id}`, tvdbId)}`;
}

// As movieLogoUrl, with has_logo on the show.
export function tvLogoUrl(show: { id: number; tvdb_id: number }): string {
	return `/posters/tvshows/${show.id}/logo.png${imageVersion(`tvshows/${show.id}`, show.tvdb_id)}`;
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
