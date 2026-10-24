// One result model behind three search surfaces: the desktop command palette,
// the phone SearchScreen and the tablet SearchField panel. Each renders its
// rows differently; none of them decides what a match is.
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
import { api, type Paginated } from "./api";
import { albumPosterUrl, artistPosterUrl, bookPosterUrl, tvPosterUrl } from "./posters";
import { auth } from "./auth.svelte";
import { SETTINGS_SECTIONS } from "./settings-nav.svelte";
import { fold } from "./text";
import type { EpisodeSearchHit, Movie, Person, TVShow } from "./types";
import {
	kindLabel,
	releaseTypeLabel,
	type AlbumSearchHit,
	type Artist,
	type BookKind,
	type ReleaseType,
	type ShelfItem,
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
// An album and an episode have no page of their own: an album opens its
// artist on that release, an episode its series on that episode's sheet.
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
export type EpisodeItem = {
	kind: "episode";
	id: number;
	label: string;
	seriesId: number;
	series: string;
	season: number;
	code: string;
};
export type SearchItem =
	| PageItem
	| ActionItem
	| MovieItem
	| SeriesItem
	| EpisodeItem
	| ArtistItem
	| AlbumItem
	| BookItem
	| PersonItem;
export type TitleItem = MovieItem | SeriesItem | EpisodeItem | ArtistItem | AlbumItem | BookItem;
// The kinds that lead to something in the library.
export const isTitleItem = (i: SearchItem): i is TitleItem =>
	i.kind === "movie" ||
	i.kind === "series" ||
	i.kind === "episode" ||
	i.kind === "artist" ||
	i.kind === "album" ||
	i.kind === "book";
export type SectionId =
	| "titles"
	| "movies"
	| "series"
	| "episodes"
	| "music"
	| "albums"
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

const ADMIN_PAGES = new Set([
	"/torrents",
	"/transcoding",
	"/imports",
	"/settings",
]);

const PAGES: PageItem[] = [
	{ kind: "page", label: i18n.nav_dashboard(), path: "/", icon: LayoutDashboard },
	{ kind: "page", label: i18n.movies_label(), path: "/movies", icon: Film },
	{ kind: "page", label: i18n.settings_series(), path: "/series", icon: Tv },
	{ kind: "page", label: i18n.music_label(), path: "/music", icon: Music },
	{ kind: "page", label: i18n.books_label(), path: "/books", icon: BookOpen },
	{ kind: "page", label: i18n.requests_label(), path: "/requests", icon: Inbox },
	{ kind: "page", label: i18n.nav_activity(), path: "/activity", icon: Activity },
	{ kind: "page", label: i18n.torrent_label(), path: "/torrents", icon: Magnet },
	{ kind: "page", label: i18n.transcode_label(), path: "/transcoding", icon: Replace },
	{ kind: "page", label: i18n.imports_label(), path: "/imports", icon: FolderInput },
	{ kind: "page", label: i18n.common_calendar(), path: "/calendar", icon: CalendarDays },
	{ kind: "page", label: i18n.nav_settings(), path: "/settings", icon: Settings },
	{ kind: "page", label: i18n.common_account(), path: "/account", icon: User },
];

export function itemKindLabel(item: SearchItem): string {
	if (item.kind === "page") return i18n.search_kind_navigate();
	if (item.kind === "action") return i18n.common_action();
	if (item.kind === "person") return i18n.common_person();
	if (item.kind === "artist") return i18n.common_artist();
	if (item.kind === "album") return releaseTypeLabel(item.releaseType);
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

function openAddMovie() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
}
function openAddSeries() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
}
function openAddArtist() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-artist"));
}
function openAddBook() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-book"));
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
	// SearchField sits in the global layout, so this model is constructed on
	// every route. Ungated, each mount pulled the whole movie and series
	// libraries for a panel that stays closed until the user types — on a
	// 23-show library that was 2.8 MB and ~2.6s of the /movies page load.
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

	function titleSearch<T>(path: string) {
		return createQuery(() => ({
			queryKey: [path.slice(1), "search", typed],
			queryFn: () =>
				api<Paginated<T>>(
					`${path}?query=${encodeURIComponent(typed)}&limit=${TITLE_LIMIT}`,
				),
			staleTime: 30_000,
			// Keeps the last hits on screen while the next ones load, so the
			// list doesn't empty and re-fill under the cursor on every keystroke.
			placeholderData: keepPreviousData,
			enabled,
		}));
	}

	const moviesQuery = titleSearch<Movie>("/movies");
	const seriesQuery = titleSearch<TVShow>("/series");
	const episodesQuery = titleSearch<EpisodeSearchHit>("/episodes");
	const artistsQuery = titleSearch<Artist>("/music/artists");
	const albumsQuery = titleSearch<AlbumSearchHit>("/music/albums");
	const booksQuery = titleSearch<ShelfItem>("/books");
	const peopleQuery = titleSearch<Person>("/people");

	// Settings sections join the pages once something is typed: listed on an
	// empty palette they would bury every other page under eighteen rows.
	function pages(typed: boolean): PageItem[] {
		const isAdmin = auth.user?.role === "admin";
		const base = PAGES.filter((p) => isAdmin || !ADMIN_PAGES.has(p.path));
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
	function actions(): ActionItem[] {
		const direct = auth.canAddDirectly;
		return [
			{
				kind: "action",
				label: direct ? i18n.search_add_movie() : i18n.search_request_movie(),
				icon: Film,
				run: openAddMovie,
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_series() : i18n.search_request_series(),
				icon: Tv,
				run: openAddSeries,
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_artist() : i18n.search_request_artist(),
				icon: Music,
				run: openAddArtist,
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_book() : i18n.search_request_book(),
				icon: BookOpen,
				run: openAddBook,
			},
			{ kind: "action", label: i18n.common_sign_out(), icon: LogOut, run: signOut },
		];
	}

	let hiddenArtists = $derived(
		enabled
			? foldedArtists(
					artistsQuery.data?.items ?? [],
					albumsQuery.data?.items ?? [],
					fold(getQuery().trim()),
				)
			: new Set<number>(),
	);

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
		const albumHits: AlbumItem[] = enabled
			? (albumsQuery.data?.items ?? []).map((a) => ({
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

	let flat = $derived(sections.flatMap((s) => s.items));
	let titleHits = $derived(flat.filter(isTitleItem).length);
	// How many titles matched, against the at-most-TITLE_LIMIT-each shown. Null
	// until the query passes TITLE_MIN and the fetch lands, so the hint stays
	// absent rather than claiming zero while the panel is still closed.
	let searchable = $derived.by<number | null>(() => {
		const totals = [
			moviesQuery,
			seriesQuery,
			episodesQuery,
			artistsQuery,
			albumsQuery,
			booksQuery,
		].map((q) => q.data?.total ?? null);
		if (totals.every((t) => t === null)) return null;
		return totals.reduce<number>((n, t) => n + (t ?? 0), 0) - hiddenArtists.size;
	});

	return {
		get sections(): SearchSection[] {
			return sections;
		},
		get flat(): SearchItem[] {
			return flat;
		},
		get titleHits(): number {
			return titleHits;
		},
		get searchable(): number | null {
			return searchable;
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
		if (!navigate) return;
		if (item.kind === "page") navigate(item.path);
		else if (item.kind === "movie")
			navigate("/movies/[id]", { id: String(item.id) });
		else if (item.kind === "person")
			navigate("/people/[id]", { id: String(item.id) });
		else if (item.kind === "artist")
			navigate("/music/[id]", { id: String(item.id) });
		else if (item.kind === "album")
			navigate("/music/[id]", { id: String(item.artistId), release: String(item.id) });
		else if (item.kind === "episode")
			navigate("/series/[id]", {
				id: String(item.seriesId),
				tab: "episodes",
				season: String(item.season),
				episode: String(item.id),
			});
		else if (item.kind === "book")
			navigate(item.series ? "/books/series/[id]" : "/books/[id]", { id: String(item.id) });
		else navigate("/series/[id]", { id: String(item.id) });
	};
}
