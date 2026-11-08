// Outbound links for a library title, built only from ids it carries. A site
// with no id-keyed address gets a title search (search: true); SensCritique
// and AlloCiné only in French.

import { getLocale } from "./paraglide/runtime.js";

// Render order; a hairline between groups.
export const LINK_GROUPS = ["database", "listen", "stores", "reviews"] as const;
export type ExternalLinkGroup = (typeof LINK_GROUPS)[number];

export type ExternalLink = {
	// Stable key.
	site: string;
	// The site's name, never translated.
	label: string;
	href: string;
	group: ExternalLinkGroup;
	// A title search, not the title's page.
	search?: boolean;
	// The search query, for the accessible label.
	query?: string;
};

// The API's ExternalIds: every id a title is known by, keyed by site.
export type ExternalIds = {
	imdb?: string;
	// A movie's own TMDB id, or a series' TMDB TV id.
	tmdb?: string;
	tvdb?: string;
	musicbrainz?: string;
	hardcover?: string;
	// Book edition ids, from Hardcover's mappings.
	goodreads?: string;
	// OL…M
	openlibrary?: string;
	google_books?: string;
	// ASIN:region
	audible?: string;
	wikidata?: string;
	discogs?: string;
	allmusic?: string;
	spotify?: string;
	deezer?: string;
	bandcamp?: string;
	lastfm?: string;
	rateyourmusic?: string;
};

export type MovieLinkSource = {
	tmdb_id: number;
	external_ids?: ExternalIds;
	title: string;
	original_title?: string;
};

export type SeriesLinkSource = {
	tvdb_id: number;
	external_ids?: ExternalIds;
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

// Latin script, digits and punctuation only.
const LATIN_TITLE = /^[\p{Script=Latin}\p{Script=Common}\p{Script=Inherited}]+$/u;

// Rotten Tomatoes and Metacritic search the original title unless it is in
// another script; the French sites the displayed one.
function reviewSearches(title: string, original: string): ExternalLink[] {
	const o = original.trim();
	const intl = o && LATIN_TITLE.test(o) ? o : title;
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
	const x = m.external_ids ?? {};
	const links: ExternalLink[] = [];
	if (x.imdb) {
		links.push({ site: "imdb", label: "IMDb", href: imdbTitleUrl(x.imdb), group: "database" });
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
	if (x.wikidata) {
		links.push({
			site: "wikidata",
			label: "Wikidata",
			href: wikidataUrl(x.wikidata),
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
	const x = s.external_ids ?? {};
	const tmdbId = Number(x.tmdb) || 0;
	const links: ExternalLink[] = [];
	if (x.imdb) {
		links.push({ site: "imdb", label: "IMDb", href: imdbTitleUrl(x.imdb), group: "database" });
	}
	if (s.tvdb_id) {
		links.push({ site: "tvdb", label: "TVDB", href: tvdbSeriesUrl(s.tvdb_id), group: "database" });
	}
	if (tmdbId) {
		links.push({ site: "tmdb", label: "TMDB", href: tmdbUrl("tv", tmdbId), group: "database" });
	}
	if (s.tvdb_id) {
		links.push({
			site: "trakt",
			label: "Trakt",
			href: `https://trakt.tv/search/tvdb/${s.tvdb_id}?id_type=show`,
			group: "database",
		});
	}
	if (x.wikidata) {
		links.push({
			site: "wikidata",
			label: "Wikidata",
			href: wikidataUrl(x.wikidata),
			group: "database",
		});
	}
	links.push(...reviewSearches(s.title, s.original_title ?? ""));
	return links;
}

// ---- Music ----
// A review site MusicBrainz links nothing for gets a search.

export type ArtistLinkSource = { mbid?: string; name: string; external_ids?: ExternalIds };
export type ReleaseLinkSource = { mbid?: string; title: string; external_ids?: ExternalIds };

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
	// Already URL-encoded.
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

// artist narrows the album searches.
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

// ---- Books ----
// The followed edition's id first, then any edition's, then the ISBN-13,
// then a search.

export type BookEditionLinkSource = {
	id: number;
	language: string;
	format: "ebook" | "audiobook";
	isbn_13?: string;
	asin?: string;
	external_ids?: ExternalIds;
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

// The slot's edition, then the reader's language, then any, if it has the id.
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

// Ebook first, then audiobook, then any.
function anyEdition(b: BookLinkSource, has: (e: BookEditionLinkSource) => string | undefined) {
	return editionWith(b, "ebook", has) ?? editionWith(b, "audiobook", has) ?? editionWith(b, undefined, has);
}

function amazonHost(): string {
	return getLocale() === "fr" ? "www.amazon.fr" : "www.amazon.com";
}

// Audible stores by mapping region.
const AUDIBLE_HOSTS: Record<string, string> = {
	us: "www.audible.com",
	uk: "www.audible.co.uk",
	de: "www.audible.de",
	fr: "www.audible.fr",
	ca: "www.audible.ca",
	au: "www.audible.com.au",
	jp: "www.audible.co.jp",
	es: "www.audible.es",
	it: "www.audible.it",
	br: "www.audible.com.br",
	in: "www.audible.in",
};

// A mapped ASIN:region opens that store, a bare ASIN the reader's.
function audibleUrl(mapped: string | undefined, asin: string | undefined): string | undefined {
	if (mapped) {
		const [id, region] = mapped.split(":");
		const host = AUDIBLE_HOSTS[region ?? ""];
		if (id && host) return `https://${host}/pd/${q(id)}`;
	}
	if (asin) return `https://${getLocale() === "fr" ? "www.audible.fr" : "www.audible.com"}/pd/${q(asin)}`;
	return undefined;
}

export function bookLinks(b: BookLinkSource): ExternalLink[] {
	const query = b.author ? `${b.title} ${b.author}` : b.title;
	const isbn = anyEdition(b, (e) => e.isbn_13);
	const goodreads = anyEdition(b, (e) => e.external_ids?.goodreads);
	const openlibrary = anyEdition(b, (e) => e.external_ids?.openlibrary);
	const google = anyEdition(b, (e) => e.external_ids?.google_books);
	const wikidata = anyEdition(b, (e) => e.external_ids?.wikidata);
	const ebookAsin = editionWith(b, "ebook", (e) => e.asin);
	const audible = audibleUrl(
		editionWith(b, "audiobook", (e) => e.external_ids?.audible),
		editionWith(b, "audiobook", (e) => e.asin),
	);
	const links: ExternalLink[] = [];
	if (b.hardcover_id) {
		links.push({ site: "hardcover", label: "Hardcover", href: `https://hardcover.app/book/${b.hardcover_id}`, group: "database" });
	}
	if (openlibrary) {
		links.push({ site: "openlibrary", label: "Open Library", href: `https://openlibrary.org/books/${q(openlibrary)}`, group: "database" });
	} else if (isbn) {
		links.push({ site: "openlibrary", label: "Open Library", href: `https://openlibrary.org/isbn/${q(isbn)}`, group: "database" });
	} else {
		links.push(search("openlibrary", "Open Library", `https://openlibrary.org/search?q=${q(query)}`, "database", query));
	}
	if (google) {
		links.push({ site: "google_books", label: "Google Books", href: `https://books.google.com/books?id=${q(google)}`, group: "database" });
	}
	if (wikidata) links.push({ site: "wikidata", label: "Wikidata", href: wikidataUrl(wikidata), group: "database" });
	if (ebookAsin) links.push({ site: "amazon", label: "Amazon", href: `https://${amazonHost()}/dp/${q(ebookAsin)}`, group: "stores" });
	if (audible) links.push({ site: "audible", label: "Audible", href: audible, group: "stores" });
	if (goodreads) {
		links.push({ site: "goodreads", label: "Goodreads", href: `https://www.goodreads.com/book/show/${q(goodreads)}`, group: "reviews" });
	} else if (isbn) {
		links.push({ site: "goodreads", label: "Goodreads", href: `https://www.goodreads.com/book/isbn/${q(isbn)}`, group: "reviews" });
	} else {
		links.push(search("goodreads", "Goodreads", `https://www.goodreads.com/search?q=${q(query)}`, "reviews", query));
	}
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
