import { createQuery } from "@tanstack/svelte-query";
import { api, apiAllPages } from "./api";
import { auth } from "./auth.svelte";
import { NAV_POLL_MS, SILENT } from "./query";
import type {
	MetadataChange,
	MetadataChangeCounts,
	MetadataChangeKind,
	MetadataChangeOwnerType,
} from "./types";

export type ChangeMedia = "music" | "series" | "movies" | "books";

// The kinds that take the item itself out of the provider: the cover reads
// "Removed" for these, "Changed" for the rest.
const ITEM_REMOVED: ReadonlySet<MetadataChangeKind> = new Set([
	"album_removed",
	"artist_removed",
	"series_removed",
	"movie_removed",
	"book_removed",
]);

export const isItemRemoved = (k: MetadataChangeKind) => ITEM_REMOVED.has(k);

export const mediaOf = (t: MetadataChangeOwnerType): ChangeMedia =>
	t === "tv_show" ? "series" : t === "movie" ? "movies" : t === "book" ? "books" : "music";

export function ownerHref(c: MetadataChange): string {
	const o = c.owner;
	switch (o.type) {
		case "album":
			return `/music/${o.artist_id}?release=${o.id}`;
		case "artist":
			return `/music/${o.id}`;
		case "tv_show":
			return `/series/${o.id}`;
		case "movie":
			return `/movies/${o.id}`;
		case "book":
			return `/books/${o.id}`;
	}
}

// Counts for the nav badge and the dashboard chip, polled with the rest of the
// nav. Admins only: the endpoints answer 403 to anyone else.
export function changeCountsQuery() {
	return createQuery<MetadataChangeCounts>(() => ({
		queryKey: ["metadata-changes", "counts"],
		queryFn: () => api<MetadataChangeCounts>("/metadata-changes/counts"),
		meta: SILENT,
		enabled: auth.isAdmin,
		retry: false,
		refetchInterval: NAV_POLL_MS,
	}));
}

// Every pending change. The list page filters it, and a cover or a detail page
// finds its own item's changes in it, so one request serves them all.
export function pendingChangesQuery() {
	return createQuery<{ items: MetadataChange[]; total: number }>(() => ({
		queryKey: ["metadata-changes", "list"],
		queryFn: () => apiAllPages<MetadataChange>("/metadata-changes"),
		meta: SILENT,
		enabled: auth.isAdmin,
		retry: false,
	}));
}

export function changesFor(
	all: MetadataChange[] | undefined,
	type: MetadataChangeOwnerType,
	id: number,
): MetadataChange[] {
	return (all ?? []).filter((c) => c.owner.type === type && c.owner.id === id);
}

export type CoverMark = "changed" | "removed";

// The mark a cover carries, given its item's pending changes.
export function markOf(changes: MetadataChange[]): CoverMark | undefined {
	if (changes.length === 0) return undefined;
	return changes.some((c) => isItemRemoved(c.kind)) ? "removed" : "changed";
}

// A cover lookup for a grid: one map built per list rather than a filter per
// card. An artist carries its albums' changes too, so the music list shows
// where to look.
export function markIndex(all: MetadataChange[] | undefined) {
	const by = new Map<string, MetadataChange[]>();
	const add = (key: string, c: MetadataChange) => by.set(key, [...(by.get(key) ?? []), c]);
	for (const c of all ?? []) {
		add(`${c.owner.type}:${c.owner.id}`, c);
		if (c.owner.type === "album" && c.owner.artist_id) add(`artist:${c.owner.artist_id}`, c);
	}
	return (type: MetadataChangeOwnerType, id: number) =>
		markOf(by.get(`${type}:${id}`) ?? []);
}

export const followChange = (id: number) =>
	api(`/metadata-changes/${id}/follow`, { method: "POST" });
export const keepChange = (id: number) =>
	api(`/metadata-changes/${id}/keep`, { method: "POST" });
