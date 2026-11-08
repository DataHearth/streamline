// One result model behind both search surfaces: the command palette for a
// mouse or trackpad, and the search page (routes/search.svelte) for touch
// screens and narrow windows. Each renders its rows differently; neither
// decides what a match is.
//
// `compact: true` is the touch shape. Movie and series hits merge into one
// "Titles" group and lead the list, because on a phone what you came for is
// almost always a title and three eyebrows above the first poster is three too
// many. The palette keeps its own order (pages and actions first), where a
// keyboard makes jumping the point.

import { onMount, type Component } from "svelte";
import { goto } from "@roxi/routify";
import { createQuery, keepPreviousData } from "@tanstack/svelte-query";
import {
	LayoutDashboard,
	Film,
	Tv,
	Music,
	BookOpen,
	Inbox,
	Activity,
	Magnet,
	Replace,
	FolderInput,
	CalendarDays,
	Settings,
	User,
	LogOut,
} from "@lucide/svelte";
import { api } from "./api";
import { albumPosterUrl, artistPosterUrl, bookPosterUrl } from "./posters";
import { activityFeatures } from "./activity-nav";
import { openAdd, type AddKind } from "./add-flow";
import { auth } from "./auth.svelte";
import { SETTINGS_SECTIONS } from "./settings-nav.svelte";
import { fold } from "./text";
import { gotoArgs, ROUTE } from "./routes";
import type { EpisodeSearchHit, Movie, Person, TVShow } from "./types";
import {
	kindLabel,
	releaseTypeLabel,
	type AlbumSearchHit,
	type Artist,
	type BookKind,
	type ReleaseType,
	type ShelfItem,
	type TrackSearchHit,
} from "./music-books";
import { volumesCount } from "./music-books-lookup";
import { m as i18n } from "./paraglide/messages.js";

export type PageItem = {
	kind: "page";
	label: string;
	path: string;
	icon: Component;
};
export type ActionItem = {
	kind: "action";
	label: string;
	icon: Component;
	run: () => void;
};
export type MovieItem = {
	kind: "movie";
	id: number;
	label: string;
	year?: number;
};
export type SeriesItem = {
	kind: "series";
	id: number;
	label: string;
	year?: number;
};
export type PersonItem = {
	kind: "person";
	id: number;
	label: string;
	profile_url?: string;
	credits: number;
};
// Music and book rows carry their own art: unlike a movie poster, there is no
// URL to build from the id.
export type ArtistItem = {
	kind: "artist";
	id: number;
	label: string;
	image?: string;
	genre?: string;
	// The album the query matched when the name did not: the list searches
	// album titles too, and an artist row with no reason on it reads as noise.
	album?: string;
};
export type BookItem = {
	kind: "book";
	id: number;
	label: string;
	image?: string;
	author: string;
	year?: number;
	bookKind: BookKind;
	// A series folds into one row, as it folds into one card on the shelf, and
	// says how many volumes it has where a book says its year.
	series: boolean;
	volumes?: number;
};
// An album, a track and an episode have no page of their own: an album or a
// track opens its artist on that release, an episode its series on that
// episode's sheet.
export type AlbumItem = {
	kind: "album";
	id: number;
	label: string;
	image: string;
	artistId: number;
	artist: string;
	year?: string;
	releaseType: ReleaseType;
};
export type TrackItem = {
	kind: "track";
	id: number;
	label: string;
	image: string;
	artistId: number;
	artist: string;
	albumId: number;
	album: string;
};
export type EpisodeItem = {
	kind: "episode";
	id: number;
	label: string;
	seriesId: number;
	series: string;
	season: number;
	code: string;
};
// Names the kind of title, never the provider: that is the backend's to change.
// `label` is the whole sentence, read out; `chip` the kind the strip shows.
export type LookupItem = {
	kind: "lookup";
	media: AddKind;
	label: string;
	chip: string;
	query: string;
	icon: Component;
};
export type SearchItem =
	| PageItem
	| ActionItem
	| MovieItem
	| SeriesItem
	| EpisodeItem
	| ArtistItem
	| AlbumItem
	| TrackItem
	| BookItem
	| PersonItem
	| LookupItem;
export type TitleItem =
	| MovieItem
	| SeriesItem
	| EpisodeItem
	| ArtistItem
	| AlbumItem
	| TrackItem
	| BookItem;
// The kinds that lead to something in the library.
export const isTitleItem = (i: SearchItem): i is TitleItem =>
	i.kind === "movie" ||
	i.kind === "series" ||
	i.kind === "episode" ||
	i.kind === "artist" ||
	i.kind === "album" ||
	i.kind === "track" ||
	i.kind === "book";
export type SectionId =
	| "titles"
	| "movies"
	| "series"
	| "episodes"
	| "music"
	| "albums"
	| "singles"
	| "eps"
	| "tracks"
	| "books"
	| "people"
	| "pages"
	| "actions";
export type SearchSection = {
	id: SectionId;
	label: string;
	items: SearchItem[];
};

// Two characters before either library is searched: one letter matches most of
// a library and the list it produces is worthless.
const TITLE_MIN = 2;
const TITLE_LIMIT = 5;

const ADMIN_PAGES = new Set<string>([ROUTE.torrents, ROUTE.transcoding, ROUTE.imports, ROUTE.settings]);

const PAGES: PageItem[] = [
	{ kind: "page", label: i18n.nav_dashboard(), path: ROUTE.home, icon: LayoutDashboard },
	{ kind: "page", label: i18n.movies_label(), path: ROUTE.movies, icon: Film },
	{ kind: "page", label: i18n.settings_series(), path: ROUTE.shows, icon: Tv },
	{ kind: "page", label: i18n.music_label(), path: ROUTE.music, icon: Music },
	{ kind: "page", label: i18n.books_label(), path: ROUTE.books, icon: BookOpen },
	{ kind: "page", label: i18n.requests_label(), path: ROUTE.requests, icon: Inbox },
	{ kind: "page", label: i18n.nav_activity(), path: ROUTE.activity, icon: Activity },
	{ kind: "page", label: i18n.torrent_label(), path: ROUTE.torrents, icon: Magnet },
	{ kind: "page", label: i18n.transcode_label(), path: ROUTE.transcoding, icon: Replace },
	{ kind: "page", label: i18n.imports_label(), path: ROUTE.imports, icon: FolderInput },
	{ kind: "page", label: i18n.common_calendar(), path: ROUTE.calendar, icon: CalendarDays },
	{ kind: "page", label: i18n.nav_settings(), path: ROUTE.settings, icon: Settings },
	{ kind: "page", label: i18n.common_account(), path: ROUTE.account, icon: User },
];

// One row per add flow: its icon, the palette action that opens it empty, and
// the lookup chip that opens it already searching the query. `request*` is the
// request_only wording.
type AddFlow = {
	media: AddKind;
	icon: Component;
	chip: () => string;
	add: () => string;
	request: () => string;
	lookupAdd: (p: { query: string }) => string;
	lookupRequest: (p: { query: string }) => string;
};
const ADD_FLOWS: AddFlow[] = [
	{
		media: "movie",
		icon: Film,
		chip: i18n.search_lookup_as_movie,
		add: i18n.search_add_movie,
		request: i18n.search_request_movie,
		lookupAdd: i18n.search_lookup_add_movie,
		lookupRequest: i18n.search_lookup_request_movie,
	},
	{
		media: "series",
		icon: Tv,
		chip: i18n.search_lookup_as_series,
		add: i18n.search_add_series,
		request: i18n.search_request_series,
		lookupAdd: i18n.search_lookup_add_series,
		lookupRequest: i18n.search_lookup_request_series,
	},
	{
		media: "artist",
		icon: Music,
		chip: i18n.search_lookup_as_artist,
		add: i18n.search_add_artist,
		request: i18n.search_request_artist,
		lookupAdd: i18n.search_lookup_add_artist,
		lookupRequest: i18n.search_lookup_request_artist,
	},
	{
		media: "book",
		icon: BookOpen,
		chip: i18n.search_lookup_as_book,
		add: i18n.search_add_book,
		request: i18n.search_request_book,
		lookupAdd: i18n.search_lookup_add_book,
		lookupRequest: i18n.search_lookup_request_book,
	},
];

export function itemKindLabel(item: SearchItem): string {
	if (item.kind === "page") return i18n.search_kind_navigate();
	if (item.kind === "lookup") return item.chip;
	if (item.kind === "action") return i18n.common_action();
	if (item.kind === "person") return i18n.common_person();
	if (item.kind === "artist") return i18n.common_artist();
	if (item.kind === "album") return releaseTypeLabel(item.releaseType);
	if (item.kind === "track") return i18n.common_track();
	if (item.kind === "episode") return i18n.common_episode();
	if (item.kind === "book") return kindLabel(item.bookKind);
	// The singular: the plural "Séries" is the section, not one show.
	return item.kind === "movie" ? i18n.common_movie() : i18n.lc_series();
}

// The line under a title row: its year, and for music, books and episodes who
// or what.
export function itemSubline(item: SearchItem): string {
	if (item.kind === "movie" || item.kind === "series") return item.year ? String(item.year) : "";
	if (item.kind === "album") return [item.artist, item.year].filter(Boolean).join(" · ");
	if (item.kind === "track") return `${item.artist} · ${item.album}`;
	if (item.kind === "episode") return `${item.series} · ${item.code}`;
	if (item.kind === "artist")
		return item.album ? i18n.search_via({ title: item.album }) : (item.genre ?? "");
	if (item.kind === "book") {
		// A series none of whose volumes is out yet still says it is one.
		const tail = item.series
			? item.volumes
				? volumesCount(item.volumes)
				: i18n.books_series()
			: item.year;
		return [item.author, tail].filter(Boolean).join(" · ");
	}
	return "";
}

async function signOut() {
	try {
		await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
	} finally {
		window.location.href = "/login";
	}
}

// An artist found only through an album's title says nothing that album's own
// row doesn't, once the row is listed: it is left out of the list and the count.
function foldedArtists(artists: Artist[], albums: AlbumSearchHit[], q: string): Set<number> {
	const owners = new Set(albums.map((a) => a.artist_id));
	return new Set(artists.filter((a) => owners.has(a.id) && matchedAlbum(a, q)).map((a) => a.id));
}

// A daily show numbers its episodes by air date, so the number alone names one.
function episodeCode(e: EpisodeSearchHit): string {
	if (e.series_type === "daily") return `#${e.episode}`;
	const pad = (n: number) => String(n).padStart(2, "0");
	return `S${pad(e.season)}E${pad(e.episode)}`;
}

// The server matches an artist on its name, sort name, genre or any album
// title; only the last needs saying on the row.
function matchedAlbum(a: Artist, q: string): string | undefined {
	if (!q || fold(a.name).includes(q) || fold(a.genre ?? "").includes(q)) return undefined;
	return a.albums.find((al) => fold(al.title).includes(q))?.title;
}

export function createSearchModel(
	getQuery: () => string,
	opts: { compact?: boolean } = {},
) {
	// The palette sits in the global layout, so this model is constructed on
	// every route. Ungated, each mount would pull the whole movie and series
	// libraries for a box that stays closed until the user types — on a
	// 23-show library that is 2.8 MB and ~2.6s of the /movies page load.
	//
	// Matching is the server's, same as the library lists: it searches the
	// original title too and folds accents, neither of which a filter over
	// `title` in the browser could do.
	let typed = $state("");
	$effect(() => {
		const q = getQuery().trim();
		const t = setTimeout(() => (typed = q), 200);
		return () => clearTimeout(t);
	});
	const enabled = $derived(typed.length >= TITLE_MIN);

	// `extra` is more query string, `&`-led, which also keys the cache. The
	// lists the library pages share answer a total unless told not to, and
	// nothing here shows one.
	const NO_TOTAL = "&with_total=false";
	function titleSearch<T>(path: string, extra = "") {
		return createQuery(() => ({
			queryKey: [path.slice(1), "search", typed, extra],
			queryFn: () =>
				api<{ items: T[] }>(
					`${path}?query=${encodeURIComponent(typed)}&limit=${TITLE_LIMIT}${extra}`,
				),
			staleTime: 30_000,
			// Keeps the last hits on screen while the next ones load, so the
			// list doesn't empty and re-fill under the cursor on every keystroke.
			placeholderData: keepPreviousData,
			enabled,
		}));
	}

	const moviesQuery = titleSearch<Movie>("/movies", NO_TOTAL);
	const seriesQuery = titleSearch<TVShow>("/series", NO_TOTAL);
	const episodesQuery = titleSearch<EpisodeSearchHit>("/episodes");
	const artistsQuery = titleSearch<Artist>("/music/artists", NO_TOTAL);
	// Each release type gets its own section and its own TITLE_LIMIT, or an
	// artist's dozen singles would push every album out of one shared list.
	const albumsQuery = titleSearch<AlbumSearchHit>(
		"/music/albums",
		"&type=album&type=live&type=compilation&type=other",
	);
	const singlesQuery = titleSearch<AlbumSearchHit>("/music/albums", "&type=single");
	const epsQuery = titleSearch<AlbumSearchHit>("/music/albums", "&type=ep");
	const tracksQuery = titleSearch<TrackSearchHit>("/music/tracks");
	const booksQuery = titleSearch<ShelfItem>("/books", NO_TOTAL);
	const peopleQuery = titleSearch<Person>("/people");
	// Torrents and transcoding are pages only while their feature is on, as in
	// the nav.
	const features = activityFeatures();

	// Settings sections join the pages once something is typed: listed on an
	// empty palette they would bury every other page under eighteen rows.
	function pages(typed: boolean): PageItem[] {
		const isAdmin = auth.user?.role === "admin";
		const base = PAGES.filter(
			(p) =>
				(isAdmin || !ADMIN_PAGES.has(p.path)) &&
				(p.path !== ROUTE.torrents || features.torrents) &&
				(p.path !== ROUTE.transcoding || features.transcoding),
		);
		if (isAdmin && typed) {
			for (const [path, { Icon, title }] of Object.entries(SETTINGS_SECTIONS)) {
				base.push({
					kind: "page",
					label: i18n.search_settings_section({ section: title() }),
					path,
					icon: Icon,
				});
			}
		}
		return base;
	}

	// request_only users request rather than add, so the labels adapt.
	function addActions(): ActionItem[] {
		const direct = auth.canAddDirectly;
		return ADD_FLOWS.map((f) => ({
			kind: "action",
			label: direct ? f.add() : f.request(),
			icon: f.icon,
			run: () => openAdd(f.media),
		}));
	}

	function actions(): ActionItem[] {
		return [
			...addActions(),
			{ kind: "action", label: i18n.common_sign_out(), icon: LogOut, run: signOut },
		];
	}

	let hiddenArtists = $derived(
		enabled
			? foldedArtists(
					artistsQuery.data?.items ?? [],
					[albumsQuery, singlesQuery, epsQuery].flatMap((x) => x.data?.items ?? []),
					fold(getQuery().trim()),
				)
			: new Set<number>(),
	);
	// The live query, not the debounced one: nothing is fetched until a pick.
	function lookups(query: string): LookupItem[] {
		if (query.length < TITLE_MIN) return [];
		// Typing the start of "Add movie…" is asking for that action, not for a
		// title named "add movie" — the strip would offer to add one. The start
		// only, so a title called "Movie" still gets the strip.
		const q = fold(query);
		if (addActions().some((a) => fold(a.label).startsWith(q))) return [];
		const direct = auth.canAddDirectly;
		return ADD_FLOWS.map((f) => ({
			kind: "lookup",
			media: f.media,
			label: direct ? f.lookupAdd({ query }) : f.lookupRequest({ query }),
			chip: f.chip(),
			query,
			icon: f.icon,
		}));
	}

	let sections = $derived.by<SearchSection[]>(() => {
		const q = fold(getQuery().trim());
		const movieHits: MovieItem[] = enabled
			? (moviesQuery.data?.items ?? []).map((m) => ({
					kind: "movie",
					id: m.id,
					label: m.title,
					year: m.year,
				}))
			: [];
		const seriesHits: SeriesItem[] = enabled
			? (seriesQuery.data?.items ?? []).map((s) => ({
					kind: "series",
					id: s.id,
					label: s.title,
					year: s.year,
				}))
			: [];
		const peopleHits: PersonItem[] = enabled
			? (peopleQuery.data?.items ?? []).map((p) => ({
					kind: "person",
					id: p.id,
					label: p.name,
					profile_url: p.profile_url,
					credits: p.credits,
				}))
			: [];
		const artistHits: ArtistItem[] = enabled
			? (artistsQuery.data?.items ?? []).map((a) => ({
					kind: "artist",
					id: a.id,
					label: a.name,
					image: artistPosterUrl(a.id),
					genre: a.genre,
					album: matchedAlbum(a, q),
				}))
			: [];
		const releaseHits = (hits: AlbumSearchHit[] | undefined): AlbumItem[] =>
			enabled
				? (hits ?? []).map((a) => ({
						kind: "album",
						id: a.id,
						label: a.title,
						image: albumPosterUrl(a.id),
						artistId: a.artist_id,
						artist: a.artist_name,
						year: a.release_date?.slice(0, 4),
						releaseType: a.type,
					}))
				: [];
		const albumHits = releaseHits(albumsQuery.data?.items);
		const singleHits = releaseHits(singlesQuery.data?.items);
		const epHits = releaseHits(epsQuery.data?.items);
		const trackHits: TrackItem[] = enabled
			? (tracksQuery.data?.items ?? []).map((t) => ({
					kind: "track",
					id: t.id,
					label: t.title,
					image: albumPosterUrl(t.album_id),
					artistId: t.artist_id,
					artist: t.artist_name,
					albumId: t.album_id,
					album: t.album_title,
				}))
			: [];
		const shownArtists = artistHits.filter((a) => !hiddenArtists.has(a.id));
		const episodeHits: EpisodeItem[] = enabled
			? (episodesQuery.data?.items ?? []).map((e) => ({
					kind: "episode",
					id: e.id,
					label: e.title,
					seriesId: e.series_id,
					series: e.series_title,
					season: e.season,
					code: episodeCode(e),
				}))
			: [];
		const bookHits: BookItem[] = enabled
			? (booksQuery.data?.items ?? []).map((b) => ({
					kind: "book",
					id: b.id,
					label: b.title,
					image: bookPosterUrl(b.cover_id),
					author: b.author,
					year: b.year,
					bookKind: b.kind,
					series: b.type === "series",
					volumes: b.volumes_out,
				}))
			: [];
		const matchedPages = pages(q !== "").filter((p) => fold(p.label).includes(q));
		const matchedActions = actions().filter((a) => fold(a.label).includes(q));

		// People sit under the titles on every surface: a name typed into search
		// is a title far more often than it is a cast member.
		const titles: SearchSection[] = [];
		if (opts.compact) {
			// The finer-grained hits follow the titles they belong to.
			const items = [
				...movieHits,
				...seriesHits,
				...shownArtists,
				...bookHits,
				...albumHits,
				...singleHits,
				...epHits,
				...trackHits,
				...episodeHits,
			];
			if (items.length) titles.push({ id: "titles", label: i18n.dash_titles(), items });
		} else {
			if (movieHits.length)
				titles.push({ id: "movies", label: i18n.movies_label(), items: movieHits });
			if (seriesHits.length)
				titles.push({ id: "series", label: i18n.settings_series(), items: seriesHits });
			if (episodeHits.length)
				titles.push({ id: "episodes", label: i18n.series_episodes(), items: episodeHits });
			if (shownArtists.length)
				titles.push({ id: "music", label: i18n.music_label(), items: shownArtists });
			if (albumHits.length)
				titles.push({ id: "albums", label: i18n.common_albums(), items: albumHits });
			if (singleHits.length)
				titles.push({ id: "singles", label: i18n.common_singles(), items: singleHits });
			if (epHits.length) titles.push({ id: "eps", label: i18n.common_eps(), items: epHits });
			if (trackHits.length)
				titles.push({ id: "tracks", label: i18n.common_tracks(), items: trackHits });
			if (bookHits.length)
				titles.push({ id: "books", label: i18n.books_label(), items: bookHits });
		}
		if (peopleHits.length)
			titles.push({ id: "people", label: i18n.people_label(), items: peopleHits });

		const rest: SearchSection[] = [];
		if (matchedPages.length)
			rest.push({ id: "pages", label: i18n.common_pages(), items: matchedPages });
		if (matchedActions.length)
			rest.push({
				id: "actions",
				label: opts.compact ? i18n.common_actions() : i18n.common_quick_actions(),
				items: matchedActions,
			});

		return opts.compact ? [...titles, ...rest] : [...rest, ...titles];
	});

	// Not a section: each surface pins them outside its scrolling list, where a
	// full library would otherwise push them out of sight. Last in `flat`, so
	// the keyboard still reaches them.
	let lookupItems = $derived(lookups(getQuery().trim()));
	let flat = $derived([...sections.flatMap((s) => s.items), ...lookupItems]);
	let matched = $derived(sections.some((s) => s.items.length > 0));
	// True from the keystroke until every search has answered for what is
	// typed: the debounce is still running, or a fetch is. Until then an empty
	// list is no verdict, and "No matches" read as one for a query the server
	// had not been asked about yet.
	let pending = $derived.by(() => {
		const q = getQuery().trim();
		if (q.length < TITLE_MIN) return false;
		if (q !== typed) return true;
		return [
			moviesQuery,
			seriesQuery,
			episodesQuery,
			artistsQuery,
			albumsQuery,
			singlesQuery,
			epsQuery,
			tracksQuery,
			booksQuery,
			peopleQuery,
		].some((x) => x.isFetching);
	});

	return {
		get pending(): boolean {
			return pending;
		},
		get sections(): SearchSection[] {
			return sections;
		},
		get flat(): SearchItem[] {
			return flat;
		},
		get matched(): boolean {
			return matched;
		},
		get lookups(): LookupItem[] {
			return lookupItems;
		},
	};
}

// Routify's goto resolves route PATTERNS (`/movies/[id]`), not concrete paths —
// passing `/movies/1` fails with "could not travel to 1".
export function searchNav() {
	let navigate: ((path: string, params?: Record<string, string>) => void) | null =
		null;
	onMount(() => goto.subscribe((fn) => (navigate = fn)));
	return function activate(item: SearchItem) {
		if (item.kind === "action") {
			item.run();
			return;
		}
		if (item.kind === "lookup") {
			openAdd(item.media, item.query);
			return;
		}
		if (!navigate) return;
		const go = (args: [string, Record<string, string>]) => navigate?.(...args);
		if (item.kind === "page") navigate(item.path);
		else if (item.kind === "movie") go(gotoArgs(ROUTE.movie, { id: item.id }));
		else if (item.kind === "person") go(gotoArgs(ROUTE.person, { id: item.id }));
		else if (item.kind === "artist") go(gotoArgs(ROUTE.artist, { id: item.id }));
		else if (item.kind === "album") go(gotoArgs(ROUTE.artist, { id: item.artistId }, { release: item.id }));
		else if (item.kind === "track") go(gotoArgs(ROUTE.artist, { id: item.artistId }, { release: item.albumId }));
		else if (item.kind === "episode")
			go(
				gotoArgs(
					ROUTE.show,
					{ id: item.seriesId },
					{ tab: "episodes", season: item.season, episode: item.id },
				),
			);
		else if (item.kind === "book") go(gotoArgs(item.series ? ROUTE.bookSeries : ROUTE.book, { id: item.id }));
		else go(gotoArgs(ROUTE.show, { id: item.id }));
	};
}
