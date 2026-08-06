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
import { createQuery } from "@tanstack/svelte-query";
import {
	LayoutDashboard,
	Film,
	Tv,
	Inbox,
	Activity,
	Magnet,
	FolderInput,
	CalendarDays,
	Settings,
	User,
	Users,
	LogOut,
} from "@lucide/svelte";
import { api } from "./api";
import { auth } from "./auth.svelte";
import type { Movie, TVShow } from "./types";

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
export type SearchItem = PageItem | ActionItem | MovieItem | SeriesItem;
export type SectionId = "titles" | "movies" | "series" | "pages" | "actions";
export type SearchSection = {
	id: SectionId;
	label: string;
	items: SearchItem[];
};

// Two characters before either library is searched: one letter matches most of
// a library and the list it produces is worthless.
const TITLE_MIN = 2;
const TITLE_LIMIT = 5;

const PAGES: PageItem[] = [
	{ kind: "page", label: "Dashboard", path: "/dashboard", icon: LayoutDashboard },
	{ kind: "page", label: "Movies", path: "/movies", icon: Film },
	{ kind: "page", label: "Series", path: "/series", icon: Tv },
	{ kind: "page", label: "Requests", path: "/requests", icon: Inbox },
	{ kind: "page", label: "Activity", path: "/activity", icon: Activity },
	{ kind: "page", label: "Torrents", path: "/activity/torrents", icon: Magnet },
	{ kind: "page", label: "Imports", path: "/library/imports", icon: FolderInput },
	{ kind: "page", label: "Calendar", path: "/calendar", icon: CalendarDays },
	{ kind: "page", label: "Settings", path: "/settings", icon: Settings },
	{ kind: "page", label: "Account", path: "/account", icon: User },
];

export function itemKindLabel(item: SearchItem): string {
	if (item.kind === "page") return "Navigate";
	if (item.kind === "action") return "Action";
	return item.kind === "movie" ? "Movie" : "Series";
}

function openAddMovie() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
}
function openAddSeries() {
	window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
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
	const moviesQuery = createQuery(() => ({
		queryKey: ["movies"],
		queryFn: () => api<{ items: Movie[] }>("/movies?page=1&limit=500"),
		staleTime: 30_000,
	}));

	const seriesQuery = createQuery(() => ({
		queryKey: ["series"],
		queryFn: () => api<{ items: TVShow[] }>("/series?page=1&limit=500"),
		staleTime: 30_000,
	}));

	function pages(): PageItem[] {
		const isAdmin = auth.user?.role === "admin";
		// Imports is admin-only.
		const base = PAGES.filter((p) => isAdmin || p.path !== "/library/imports");
		if (isAdmin) {
			base.push({
				kind: "page",
				label: "Users",
				path: "/settings/users",
				icon: Users,
			});
		}
		return base;
	}

	// request_only users request rather than add, so the labels adapt.
	function actions(): ActionItem[] {
		const verb = auth.canAddDirectly ? "Add" : "Request";
		return [
			{ kind: "action", label: `${verb} movie…`, icon: Film, run: openAddMovie },
			{ kind: "action", label: `${verb} series…`, icon: Tv, run: openAddSeries },
			{ kind: "action", label: "Sign out", icon: LogOut, run: signOut },
		];
	}

	let sections = $derived.by<SearchSection[]>(() => {
		const q = getQuery().trim().toLowerCase();
		const movieHits: MovieItem[] =
			q.length >= TITLE_MIN && moviesQuery.data
				? moviesQuery.data.items
						.filter((m) => m.title.toLowerCase().includes(q))
						.slice(0, TITLE_LIMIT)
						.map((m) => ({
							kind: "movie",
							id: m.id,
							label: m.title,
							year: m.year,
						}))
				: [];
		const seriesHits: SeriesItem[] =
			q.length >= TITLE_MIN && seriesQuery.data
				? seriesQuery.data.items
						.filter((s) => s.title.toLowerCase().includes(q))
						.slice(0, TITLE_LIMIT)
						.map((s) => ({
							kind: "series",
							id: s.id,
							label: s.title,
							year: s.year,
						}))
				: [];
		const matchedPages = pages().filter((p) =>
			p.label.toLowerCase().includes(q),
		);
		const matchedActions = actions().filter((a) =>
			a.label.toLowerCase().includes(q),
		);

		const titles: SearchSection[] = [];
		if (opts.compact) {
			const items = [...movieHits, ...seriesHits];
			if (items.length) titles.push({ id: "titles", label: "Titles", items });
		} else {
			if (movieHits.length)
				titles.push({ id: "movies", label: "Movies", items: movieHits });
			if (seriesHits.length)
				titles.push({ id: "series", label: "Series", items: seriesHits });
		}

		const rest: SearchSection[] = [];
		if (matchedPages.length)
			rest.push({ id: "pages", label: "Pages", items: matchedPages });
		if (matchedActions.length)
			rest.push({
				id: "actions",
				label: opts.compact ? "Actions" : "Quick actions",
				items: matchedActions,
			});

		return opts.compact ? [...titles, ...rest] : [...rest, ...titles];
	});

	let flat = $derived(sections.flatMap((s) => s.items));
	let titleHits = $derived(
		flat.filter((i) => i.kind === "movie" || i.kind === "series").length,
	);
	// What search actually looks at, not what the library holds: both queries
	// take one page of 500. Above that the hint understates, which is the safe
	// direction for a line that exists to say "there is more behind this".
	let searchable = $derived.by<number | null>(() => {
		const m = moviesQuery.data?.items.length ?? null;
		const s = seriesQuery.data?.items.length ?? null;
		if (m === null && s === null) return null;
		return (m ?? 0) + (s ?? 0);
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
		else navigate("/series/[id]", { id: String(item.id) });
	};
}
