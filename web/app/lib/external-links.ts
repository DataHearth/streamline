// Outbound links for a library title: the databases that know it by id, the
// places it can be played or bought, then the sites people read reviews on. The ids come from the provider the title
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

// Rendered in this order, a hairline between two groups.
export const LINK_GROUPS = ["database", "listen", "stores", "reviews"] as const;
export type ExternalLinkGroup = (typeof LINK_GROUPS)[number];

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

// ---- Music -----------------------------------------------------------------
//
// MusicBrainz's url relations, as GET /music/artists/{id} and the albums in it
// carry them (MusicExternalIds). Each value is what the site's address is built
// from. A review site MusicBrainz links nothing for gets a search, like
// Rotten Tomatoes for a film.

export type MusicExternalIds = {
	wikidata?: string;
	discogs?: string;
	allmusic?: string;
	spotify?: string;
	deezer?: string;
	bandcamp?: string;
	lastfm?: string;
	rateyourmusic?: string;
};

export type ArtistLinkSource = { mbid?: string; name: string; external_ids?: MusicExternalIds };
export type ReleaseLinkSource = { mbid?: string; title: string; external_ids?: MusicExternalIds };

function search(site: string, label: string, href: string, group: ExternalLinkGroup, query: string): ExternalLink {
	return { site, label, href, group, search: true, query };
}

export function artistLinks(a: ArtistLinkSource): ExternalLink[] {
	const x = a.external_ids ?? {};
	const links: ExternalLink[] = [];
	if (a.mbid) {
		links.push({ site: "musicbrainz", label: "MusicBrainz", href: `https://musicbrainz.org/artist/${q(a.mbid)}`, group: "database" });
	}
	links.push(
		x.discogs
			? { site: "discogs", label: "Discogs", href: `https://www.discogs.com/artist/${q(x.discogs)}`, group: "database" }
			: search("discogs", "Discogs", `https://www.discogs.com/search/?type=artist&q=${q(a.name)}`, "database", a.name),
	);
	// Last.fm's segment is already written the way its URL writes it.
	if (x.lastfm) links.push({ site: "lastfm", label: "Last.fm", href: `https://www.last.fm/music/${x.lastfm}`, group: "database" });
	if (x.wikidata) links.push({ site: "wikidata", label: "Wikidata", href: wikidataUrl(x.wikidata), group: "database" });
	if (x.spotify) links.push({ site: "spotify", label: "Spotify", href: `https://open.spotify.com/artist/${q(x.spotify)}`, group: "listen" });
	if (x.deezer) links.push({ site: "deezer", label: "Deezer", href: `https://www.deezer.com/artist/${q(x.deezer)}`, group: "listen" });
	if (x.bandcamp) links.push({ site: "bandcamp", label: "Bandcamp", href: `https://${x.bandcamp}/`, group: "listen" });
	links.push(
		x.allmusic
			? { site: "allmusic", label: "AllMusic", href: `https://www.allmusic.com/artist/${q(x.allmusic)}`, group: "reviews" }
			: search("allmusic", "AllMusic", `https://www.allmusic.com/search/artists/${q(a.name)}`, "reviews", a.name),
		x.rateyourmusic
			? { site: "rateyourmusic", label: "RateYourMusic", href: `https://rateyourmusic.com/artist/${x.rateyourmusic}`, group: "reviews" }
			: search("rateyourmusic", "RateYourMusic", `https://rateyourmusic.com/search?searchtype=a&searchterm=${q(a.name)}`, "reviews", a.name),
	);
	if (getLocale() === "fr") {
		links.push(search("senscritique", "SensCritique", `https://www.senscritique.com/search?query=${q(a.name)}`, "reviews", a.name));
	}
	return links;
}

// artist is the album's credited name, for the searches: an album title alone
// finds every album of that name.
export function releaseLinks(r: ReleaseLinkSource, artist: string): ExternalLink[] {
	const x = r.external_ids ?? {};
	const query = artist ? `${artist} ${r.title}` : r.title;
	const links: ExternalLink[] = [];
	if (r.mbid) {
		links.push({ site: "musicbrainz", label: "MusicBrainz", href: `https://musicbrainz.org/release-group/${q(r.mbid)}`, group: "database" });
	}
	links.push(
		x.discogs
			? { site: "discogs", label: "Discogs", href: `https://www.discogs.com/master/${q(x.discogs)}`, group: "database" }
			: search("discogs", "Discogs", `https://www.discogs.com/search/?type=master&q=${q(query)}`, "database", query),
	);
	if (x.wikidata) links.push({ site: "wikidata", label: "Wikidata", href: wikidataUrl(x.wikidata), group: "database" });
	if (x.spotify) links.push({ site: "spotify", label: "Spotify", href: `https://open.spotify.com/album/${q(x.spotify)}`, group: "listen" });
	if (x.deezer) links.push({ site: "deezer", label: "Deezer", href: `https://www.deezer.com/album/${q(x.deezer)}`, group: "listen" });
	if (x.bandcamp) links.push({ site: "bandcamp", label: "Bandcamp", href: `https://${x.bandcamp}`, group: "listen" });
	links.push(
		x.allmusic
			? { site: "allmusic", label: "AllMusic", href: `https://www.allmusic.com/album/${q(x.allmusic)}`, group: "reviews" }
			: search("allmusic", "AllMusic", `https://www.allmusic.com/search/albums/${q(query)}`, "reviews", query),
		x.rateyourmusic
			? { site: "rateyourmusic", label: "RateYourMusic", href: `https://rateyourmusic.com/release/${x.rateyourmusic}/`, group: "reviews" }
			: search("rateyourmusic", "RateYourMusic", `https://rateyourmusic.com/search?searchtype=l&searchterm=${q(query)}`, "reviews", query),
		search("metacritic", "Metacritic", `https://www.metacritic.com/search/${q(query)}/`, "reviews", query),
	);
	if (getLocale() === "fr") {
		links.push(search("senscritique", "SensCritique", `https://www.senscritique.com/search?query=${q(query)}`, "reviews", query));
	}
	return links;
}

// ---- Books -----------------------------------------------------------------
//
// Hardcover keeps no other site's id for a book, only each edition's ISBN-13
// and ASIN. Goodreads and Open Library both resolve an ISBN to the work's
// page; without one they get a search. Amazon takes a printed or ebook
// edition's ASIN, Audible an audiobook's.

export type BookEditionLinkSource = {
	id: number;
	language: string;
	format: "ebook" | "audiobook";
	isbn_13?: string;
	asin?: string;
};
export type BookLinkSource = {
	hardcover_id?: number;
	title: string;
	author: string;
	preferred_language: string;
	formats: { format: "ebook" | "audiobook"; edition_id: number | null }[];
	editions: BookEditionLinkSource[];
};
export type BookSeriesLinkSource = { hardcover_slug?: string; title: string; author: string };

// The edition a link is built from: the slot's own, then one in the reader's
// language, then any, each only if it carries the id.
function editionWith(
	b: BookLinkSource,
	format: "ebook" | "audiobook" | undefined,
	has: (e: BookEditionLinkSource) => string | undefined,
): string | undefined {
	const slotIds = b.formats.filter((f) => !format || f.format === format).map((f) => f.edition_id);
	const pool = format ? b.editions.filter((e) => e.format === format) : b.editions;
	const ordered = [
		...pool.filter((e) => slotIds.includes(e.id)),
		...pool.filter((e) => e.language === b.preferred_language),
		...pool,
	];
	for (const e of ordered) {
		const v = has(e);
		if (v) return v;
	}
	return undefined;
}

function amazonHost(): string {
	return getLocale() === "fr" ? "www.amazon.fr" : "www.amazon.com";
}
function audibleHost(): string {
	return getLocale() === "fr" ? "www.audible.fr" : "www.audible.com";
}

export function bookLinks(b: BookLinkSource): ExternalLink[] {
	const query = b.author ? `${b.title} ${b.author}` : b.title;
	const isbn = editionWith(b, "ebook", (e) => e.isbn_13) ?? editionWith(b, undefined, (e) => e.isbn_13);
	const ebookAsin = editionWith(b, "ebook", (e) => e.asin);
	const audioAsin = editionWith(b, "audiobook", (e) => e.asin);
	const links: ExternalLink[] = [];
	if (b.hardcover_id) {
		links.push({ site: "hardcover", label: "Hardcover", href: `https://hardcover.app/book/${b.hardcover_id}`, group: "database" });
	}
	links.push(
		isbn
			? { site: "openlibrary", label: "Open Library", href: `https://openlibrary.org/isbn/${q(isbn)}`, group: "database" }
			: search("openlibrary", "Open Library", `https://openlibrary.org/search?q=${q(query)}`, "database", query),
	);
	if (ebookAsin) links.push({ site: "amazon", label: "Amazon", href: `https://${amazonHost()}/dp/${q(ebookAsin)}`, group: "stores" });
	if (audioAsin) links.push({ site: "audible", label: "Audible", href: `https://${audibleHost()}/pd/${q(audioAsin)}`, group: "stores" });
	links.push(
		isbn
			? { site: "goodreads", label: "Goodreads", href: `https://www.goodreads.com/book/isbn/${q(isbn)}`, group: "reviews" }
			: search("goodreads", "Goodreads", `https://www.goodreads.com/search?q=${q(query)}`, "reviews", query),
	);
	if (getLocale() === "fr") {
		links.push(search("senscritique", "SensCritique", `https://www.senscritique.com/search?query=${q(b.title)}`, "reviews", b.title));
	}
	return links;
}

export function bookSeriesLinks(s: BookSeriesLinkSource): ExternalLink[] {
	const query = s.author ? `${s.title} ${s.author}` : s.title;
	const links: ExternalLink[] = [];
	if (s.hardcover_slug) {
		links.push({ site: "hardcover", label: "Hardcover", href: `https://hardcover.app/series/${q(s.hardcover_slug)}`, group: "database" });
	}
	links.push(search("goodreads", "Goodreads", `https://www.goodreads.com/search?q=${q(query)}`, "reviews", query));
	if (getLocale() === "fr") {
		links.push(search("senscritique", "SensCritique", `https://www.senscritique.com/search?query=${q(s.title)}`, "reviews", s.title));
	}
	return links;
}
