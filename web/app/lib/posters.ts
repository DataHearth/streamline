export type PosterRef = { id: number };

export function posterUrl(movie: PosterRef): string {
	return `/posters/movies/${movie.id}/poster.jpg`;
}

export function tvPosterUrl(id: number): string {
	return `/posters/tvshows/${id}/poster.jpg`;
}
