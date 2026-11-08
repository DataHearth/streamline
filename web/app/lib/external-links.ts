// Outbound links for a library title: the databases that know it by id, then
// the sites people read reviews on. The ids come from the provider the title
// was added from (TMDB for a movie, TVDB for a series) and from the
// cross-references that provider keeps — IMDb, TMDB, Wikidata — so a link is
// only built from an id the record actually carries.
//
// A site that resolves one of those ids gets a direct link (Trakt and
// Letterboxd both redirect from a TMDB or TVDB id). Rotten Tomatoes,
// Metacritic, SensCritique and AlloCiné have no URL keyed on any id we hold,
// so they get a search for the title and say so (`search: true`), rather than
// a guessed slug that lands on the wrong film. The two French sites are only
// offered in the French locale.

import { getLocale } from "./paraglide/runtime.js";

export type ExternalLinkGroup = "database" | "reviews";

export type ExternalLink = {
	// Stable key, also what the {#each} keys on.
	site: string;
	// The site's own name, never translated.
	label: string;
	href: string;
	group: ExternalLinkGroup;
	// True when href is a title search on the site, not the title's own page.
	search?: boolean;
	// The query a search link runs, for its accessible label.
	query?: string;
};

export type MovieLinkSource = {
	tmdb_id: number;
	imdb_id?: string;
	wikidata_id?: string;
	title: string;
	original_title?: string;
};

export type SeriesLinkSource = {
	tvdb_id: number;
	tmdb_id?: number;
	imdb_id?: string;
	wikidata_id?: string;
	title: string;
	original_title?: string;
};

const q = encodeURIComponent;

export const imdbTitleUrl = (id: string) => `https://www.imdb.com/title/${q(id)}/`;
export const tmdbUrl = (kind: "movie" | "tv", id: number) =>
	`https://www.themoviedb.org/${kind}/${id}`;
export const tvdbSeriesUrl = (id: number) =>
	`https://www.thetvdb.com/dereferrer/series/${id}`;
export const wikidataUrl = (id: string) => `https://www.wikidata.org/wiki/${q(id)}`;

// Rotten Tomatoes and Metacritic index the original (usually English) title;
// the French sites the localized one the library shows.
function reviewSearches(title: string, original: string): ExternalLink[] {
	const intl = original.trim() || title;
	const links: ExternalLink[] = [
		{
			site: "rottentomatoes",
			label: "Rotten Tomatoes",
			href: `https://www.rottentomatoes.com/search?search=${q(intl)}`,
			group: "reviews",
			search: true,
			query: intl,
		},
		{
			site: "metacritic",
			label: "Metacritic",
			href: `https://www.metacritic.com/search/${q(intl)}/`,
			group: "reviews",
			search: true,
			query: intl,
		},
	];
	if (getLocale() === "fr") {
		links.push(
			{
				site: "senscritique",
				label: "SensCritique",
				href: `https://www.senscritique.com/search?query=${q(title)}`,
				group: "reviews",
				search: true,
				query: title,
			},
			{
				site: "allocine",
				label: "AlloCiné",
				href: `https://www.allocine.fr/rechercher/?q=${q(title)}`,
				group: "reviews",
				search: true,
				query: title,
			},
		);
	}
	return links;
}

export function movieLinks(m: MovieLinkSource): ExternalLink[] {
	const links: ExternalLink[] = [];
	if (m.imdb_id) {
		links.push({ site: "imdb", label: "IMDb", href: imdbTitleUrl(m.imdb_id), group: "database" });
	}
	if (m.tmdb_id) {
		links.push(
			{ site: "tmdb", label: "TMDB", href: tmdbUrl("movie", m.tmdb_id), group: "database" },
			{
				site: "trakt",
				label: "Trakt",
				href: `https://trakt.tv/search/tmdb/${m.tmdb_id}?id_type=movie`,
				group: "database",
			},
		);
	}
	if (m.wikidata_id) {
		links.push({
			site: "wikidata",
			label: "Wikidata",
			href: wikidataUrl(m.wikidata_id),
			group: "database",
		});
	}
	if (m.tmdb_id) {
		links.push({
			site: "letterboxd",
			label: "Letterboxd",
			href: `https://letterboxd.com/tmdb/${m.tmdb_id}/`,
			group: "reviews",
		});
	}
	links.push(...reviewSearches(m.title, m.original_title ?? ""));
	return links;
}

export function seriesLinks(s: SeriesLinkSource): ExternalLink[] {
	const links: ExternalLink[] = [];
	if (s.imdb_id) {
		links.push({ site: "imdb", label: "IMDb", href: imdbTitleUrl(s.imdb_id), group: "database" });
	}
	if (s.tvdb_id) {
		links.push({ site: "tvdb", label: "TVDB", href: tvdbSeriesUrl(s.tvdb_id), group: "database" });
	}
	if (s.tmdb_id) {
		links.push({ site: "tmdb", label: "TMDB", href: tmdbUrl("tv", s.tmdb_id), group: "database" });
	}
	if (s.tvdb_id) {
		links.push({
			site: "trakt",
			label: "Trakt",
			href: `https://trakt.tv/search/tvdb/${s.tvdb_id}?id_type=show`,
			group: "database",
		});
	}
	if (s.wikidata_id) {
		links.push({
			site: "wikidata",
			label: "Wikidata",
			href: wikidataUrl(s.wikidata_id),
			group: "database",
		});
	}
	links.push(...reviewSearches(s.title, s.original_title ?? ""));
	return links;
}
