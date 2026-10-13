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
	Users,
	LogOut,
} from "@lucide/svelte";
import { api, type Paginated } from "./api";
import { openAdd, type AddKind } from "./add-flow";
import { artistPosterUrl, bookPosterUrl } from "./posters";
import { auth } from "./auth.svelte";
import { fold } from "./text";
import type { Movie, Person, TVShow } from "./types";
import { kindLabel, type Artist, type BookKind, type ShelfItem } from "./music-books";
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
};
export type BookItem = {
	kind: "book";
	id: number;
	label: string;
	image?: string;
	author: string;
	year?: number;
	bookKind: BookKind;
	// A series folds into one row, as it folds into one card on the shelf.
	series: boolean;
};
// Names the kind of title, never the provider: that is the backend's to change.
export type LookupItem = {
	kind: "lookup";
	media: AddKind;
	label: string;
	query: string;
	icon: Component;
};
export type SearchItem =
	| PageItem
	| ActionItem
	| MovieItem
	| SeriesItem
	| ArtistItem
	| BookItem
	| PersonItem
	| LookupItem;
// The four kinds that lead to a title's own page.
export const isTitleItem = (i: SearchItem): i is MovieItem | SeriesItem | ArtistItem | BookItem =>
	i.kind === "movie" || i.kind === "series" || i.kind === "artist" || i.kind === "book";
export type SectionId =
	| "titles"
	| "movies"
	| "series"
	| "music"
	| "books"
	| "people"
	| "pages"
	| "actions"
	| "lookup";
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

const ADD_KINDS: AddKind[] = ["movie", "series", "artist", "book"];
const MEDIA_ICON: Record<AddKind, Component> = {
	movie: Film,
	series: Tv,
	artist: Music,
	book: BookOpen,
};

function lookupLabel(media: AddKind, query: string, direct: boolean): string {
	if (media === "movie")
		return direct ? i18n.search_lookup_add_movie({ query }) : i18n.search_lookup_request_movie({ query });
	if (media === "series")
		return direct ? i18n.search_lookup_add_series({ query }) : i18n.search_lookup_request_series({ query });
	if (media === "artist")
		return direct ? i18n.search_lookup_add_artist({ query }) : i18n.search_lookup_request_artist({ query });
	return direct ? i18n.search_lookup_add_book({ query }) : i18n.search_lookup_request_book({ query });
}

function mediaLabel(media: AddKind): string {
	if (media === "movie") return i18n.common_movie();
	if (media === "series") return i18n.settings_series();
	if (media === "artist") return i18n.common_artist();
	return i18n.common_book();
}

export function itemKindLabel(item: SearchItem): string {
	if (item.kind === "page") return i18n.search_kind_navigate();
	if (item.kind === "lookup") return mediaLabel(item.media);
	if (item.kind === "action") return i18n.common_action();
	if (item.kind === "person") return i18n.common_person();
	if (item.kind === "artist") return i18n.common_artist();
	if (item.kind === "book") return kindLabel(item.bookKind);
	return item.kind === "movie" ? i18n.common_movie() : i18n.settings_series();
}

// The line under a title row: its year, and for music and books who or what.
export function itemSubline(item: SearchItem): string {
	if (item.kind === "movie" || item.kind === "series") return item.year ? String(item.year) : "";
	if (item.kind === "artist") return item.genre ?? "";
	if (item.kind === "book") return [item.author, item.year].filter(Boolean).join(" · ");
	return "";
}

async function signOut() {
	try {
		await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
	} finally {
		window.location.href = "/login";
	}
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
	const artistsQuery = titleSearch<Artist>("/music/artists");
	const booksQuery = titleSearch<ShelfItem>("/books");
	const peopleQuery = titleSearch<Person>("/people");

	function pages(): PageItem[] {
		const isAdmin = auth.user?.role === "admin";
		const base = PAGES.filter((p) => isAdmin || !ADMIN_PAGES.has(p.path));
		if (isAdmin) {
			base.push({
				kind: "page",
				label: i18n.settings_users(),
				path: "/settings/users",
				icon: Users,
			});
		}
		return base;
	}

	// request_only users request rather than add, so the labels adapt.
	function addActions(): ActionItem[] {
		const direct = auth.canAddDirectly;
		return [
			{
				kind: "action",
				label: direct ? i18n.search_add_movie() : i18n.search_request_movie(),
				icon: Film,
				run: () => openAdd("movie"),
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_series() : i18n.search_request_series(),
				icon: Tv,
				run: () => openAdd("series"),
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_artist() : i18n.search_request_artist(),
				icon: Music,
				run: () => openAdd("artist"),
			},
			{
				kind: "action",
				label: direct ? i18n.search_add_book() : i18n.search_request_book(),
				icon: BookOpen,
				run: () => openAdd("book"),
			},
		];
	}

	function actions(): ActionItem[] {
		return [
			...addActions(),
			{ kind: "action", label: i18n.common_sign_out(), icon: LogOut, run: signOut },
		];
	}

	// The live query, not the debounced one: nothing is fetched until a pick.
	function lookups(query: string): LookupItem[] {
		if (query.length < TITLE_MIN) return [];
		// Typing the start of "Add movie…" is asking for that action, not for a
		// title named "add movie" — the rows would offer to add one. The start
		// only, so a title called "Movie" still gets the rows.
		const q = fold(query);
		if (addActions().some((a) => fold(a.label).startsWith(q))) return [];
		return ADD_KINDS.map((media) => ({
			kind: "lookup",
			media,
			label: lookupLabel(media, query, auth.canAddDirectly),
			query,
			icon: MEDIA_ICON[media],
		}));
	}

	let sections = $derived.by<SearchSection[]>(() => {
		const typedNow = getQuery().trim();
		const q = fold(typedNow);
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
				}))
			: [];
		const matchedPages = pages().filter((p) => fold(p.label).includes(q));
		const matchedActions = actions().filter((a) => fold(a.label).includes(q));

		// People sit under the titles on every surface: a name typed into search
		// is a title far more often than it is a cast member.
		const titles: SearchSection[] = [];
		if (opts.compact) {
			const items = [...movieHits, ...seriesHits, ...artistHits, ...bookHits];
			if (items.length) titles.push({ id: "titles", label: i18n.dash_titles(), items });
		} else {
			if (movieHits.length)
				titles.push({ id: "movies", label: i18n.movies_label(), items: movieHits });
			if (seriesHits.length)
				titles.push({ id: "series", label: i18n.settings_series(), items: seriesHits });
			if (artistHits.length)
				titles.push({ id: "music", label: i18n.music_label(), items: artistHits });
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

		const lookup: SearchSection[] = [];
		const lookupItems = lookups(typedNow);
		if (lookupItems.length)
			lookup.push({
				id: "lookup",
				label: i18n.search_lookup_section(),
				items: lookupItems,
			});

		return opts.compact
			? [...titles, ...rest, ...lookup]
			: [...rest, ...titles, ...lookup];
	});

	let flat = $derived(sections.flatMap((s) => s.items));
	let titleHits = $derived(flat.filter(isTitleItem).length);
	let matched = $derived(flat.some((i) => i.kind !== "lookup"));
	let lookupShown = $derived(sections.some((s) => s.id === "lookup"));
	// How many titles matched, against the at-most-TITLE_LIMIT-each shown. Null
	// until the query passes TITLE_MIN and the fetch lands, so the hint stays
	// absent rather than claiming zero while the panel is still closed.
	let searchable = $derived.by<number | null>(() => {
		const totals = [moviesQuery, seriesQuery, artistsQuery, booksQuery].map(
			(q) => q.data?.total ?? null,
		);
		if (totals.every((t) => t === null)) return null;
		return totals.reduce<number>((n, t) => n + (t ?? 0), 0);
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
		get matched(): boolean {
			return matched;
		},
		get lookupShown(): boolean {
			return lookupShown;
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
		if (item.kind === "lookup") {
			openAdd(item.media, item.query);
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
		else if (item.kind === "book")
			navigate(item.series ? "/books/series/[id]" : "/books/[id]", { id: String(item.id) });
		else navigate("/series/[id]", { id: String(item.id) });
	};
}
