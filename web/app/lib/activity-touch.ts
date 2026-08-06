// What the touch layouts of Queue & History and Torrents share: how the ring
// reads a status, the third line each view puts under a title, and the sort the
// torrent table's column headers used to own.
//
// The ring answers two questions at once — how far along, and in what state.
// Some states make a percentage a lie: importing sits at 100% but isn't done, a
// magnet has no size to be a fraction of, and a failed grab never had progress.
// Those get a glyph instead of the number, and an arc that either spins or
// completes.

import type { StatusKind } from "../components/shared/StatusPill.svelte";
import { formatBytes, formatEta, formatRatio, formatSpeed } from "./format";
import { formatRelative } from "./dates";
import type { HistoryEntry, QueueEntry, Torrent } from "./types";
import { m as i18n } from "./paraglide/messages.js";

const episodeTokenRe = /S\d{1,2}E\d{1,2}/i;
const pad2 = (n: number) => String(n).padStart(2, "0");
const joinDot = (parts: (string | undefined | null)[]) =>
	parts.filter((p): p is string => Boolean(p && p.trim())).join(" · ");
const pct = (v: number | undefined | null) =>
	`${Math.round(Math.min(1, Math.max(0, v ?? 0)) * 100)}%`;

// Episode records carry no movie; a season pack names no specific episode, so
// its linked episode is just the season's first — label it as the season rather
// than a misleading SxxE01.
export function entryHeading(item: QueueEntry | HistoryEntry): string {
	const ep = item.episode;
	if (!ep) return item.movie.title;
	if (episodeTokenRe.test(item.title)) {
		return `${ep.show_title} · S${pad2(ep.season)}E${pad2(ep.episode)}`;
	}
	const season = ep.season === 0 ? i18n.series_specials() : i18n.season_number({ number: pad2(ep.season) });
	return `${ep.show_title} · ${season}`;
}

export type RingGlyph = "check" | "cross" | "pause" | "up" | "down" | "dots" | null;
export type RingReading = {
	// arc: the sweep is the progress. spin: indeterminate, rotates. full: a
	// complete circle the item may or may not have earned (failure included).
	mode: "arc" | "spin" | "full";
	glyph: RingGlyph;
};

export function ringReading(status: StatusKind): RingReading {
	switch (status) {
		case "grabbing":
			return { mode: "spin", glyph: "down" };
		case "fetching":
			return { mode: "spin", glyph: "dots" };
		case "paused":
			return { mode: "arc", glyph: "pause" };
		case "failed":
			return { mode: "full", glyph: "cross" };
		case "available":
		case "completed":
			return { mode: "full", glyph: "check" };
		case "seeding":
			return { mode: "full", glyph: "up" };
		default:
			// downloading, stalled — the number is true, so it stays.
			return { mode: "arc", glyph: null };
	}
}

export type MetaLine = { text: string; color?: string };

export function queueMeta(item: QueueEntry): MetaLine {
	if (item.status === "error") {
		return {
			text: item.failure_reason || "Download failed",
			color: "var(--status-failed)",
		};
	}
	if (item.status === "importing") {
		return {
			text: joinDot(["importing", item.download_client]),
			color: "var(--status-grabbing)",
		};
	}
	if (item.status === "paused") {
		const held = formatBytes(item.size * (item.progress ?? 0), "");
		return {
			text: joinDot([
				`paused at ${pct(item.progress)}`,
				held ? `${held} of ${formatBytes(item.size)}` : "",
			]),
		};
	}
	const eta = formatEta(item.eta);
	return {
		text: joinDot([
			formatSpeed(item.download_speed),
			eta ? `${eta} left` : "",
			item.download_client,
		]),
	};
}

export function historyMeta(item: HistoryEntry): MetaLine {
	const when = formatRelative(item.updated_at);
	if (item.status === "failed") {
		return {
			text: joinDot([when, item.failure_reason || "failed", item.indexer]),
			color: "var(--status-failed)",
		};
	}
	return {
		text: joinDot([when, formatBytes(item.size, ""), item.indexer]),
	};
}

export function torrentMeta(t: Torrent): MetaLine {
	const swarm = t.peer_count > 0 ? `${t.peer_count} peers` : "";
	if (t.status === "fetching") {
		return {
			text: joinDot(["waiting for metadata", swarm || "0 peers"]),
			color: "var(--fg-faint)",
		};
	}
	if (t.status === "stalled") {
		return {
			text: joinDot(["stalled", swarm || "no peers"]),
			color: "var(--status-stalled)",
		};
	}
	if (t.status === "paused") {
		return { text: joinDot([`paused at ${pct(t.progress)}`, formatBytes(t.size, "")]) };
	}
	if (t.status === "seeding") {
		const up = formatSpeed(t.upload_speed);
		return { text: joinDot([up ? `↑ ${up}` : "", formatRatio(t.ratio), swarm]) };
	}
	if (t.status === "completed") {
		return {
			text: joinDot([
				t.seeding_stopped ? "seeding stopped" : "complete",
				formatRatio(t.ratio),
				formatBytes(t.size, ""),
			]),
		};
	}
	const down = formatSpeed(t.download_speed);
	const up = formatSpeed(t.upload_speed);
	return {
		text: joinDot([
			down ? `↓ ${down}` : "",
			up ? `↑ ${up}` : "",
			formatRatio(t.ratio),
			t.seeds || t.peer_count ? `${t.seeds}/${t.peer_count}` : "",
		]),
	};
}

// ── Torrent sort ──────────────────────────────────────────────────────────
// One sort for both surfaces: the table's headers set it from md up, the filter
// sheet's chips set it below lg, and the route owns the state so the list and
// the table can never disagree.

export type TorrentSortKey =
	| "status"
	| "name"
	| "progress"
	| "size"
	| "download_speed"
	| "upload_speed"
	| "ratio"
	| "seeds";
export type SortOrder = "asc" | "desc";

// Default order: what needs attention first. Live transfers on top, then the
// stuck ones, then anything merely seeding or done.
export const TORRENT_STATUS_RANK: Record<string, number> = {
	downloading: 0,
	stalled: 1,
	paused: 2,
	seeding: 3,
	completed: 4,
};

export function compareTorrents(
	a: Torrent,
	b: Torrent,
	sort: TorrentSortKey,
): number {
	if (sort === "status") {
		const d = (TORRENT_STATUS_RANK[a.status] ?? 9) - (TORRENT_STATUS_RANK[b.status] ?? 9);
		if (d !== 0) return d;
		return (b.progress ?? 0) - (a.progress ?? 0);
	}
	if (sort === "name") return a.name.localeCompare(b.name);
	return ((a[sort] as number) ?? 0) - ((b[sort] as number) ?? 0);
}

export function sortTorrents(
	rows: Torrent[],
	sort: TorrentSortKey,
	order: SortOrder,
): Torrent[] {
	const out = [...rows].sort((a, b) => compareTorrents(a, b, sort));
	return order === "desc" ? out.reverse() : out;
}

// The sheet's chips, each pinning both key and the order that reads best for
// it — the column headers are gone below md, and with them the second tap that
// used to flip direction.
export const TORRENT_SORT_CHIPS: {
	key: string;
	label: string;
	sort: TorrentSortKey;
	order: SortOrder;
}[] = [
	{ key: "attention", label: i18n.sort_attention_first(), sort: "status", order: "asc" },
	{ key: "name", label: i18n.sort_name_az(), sort: "name", order: "asc" },
	{ key: "size", label: i18n.sort_largest(), sort: "size", order: "desc" },
	{ key: "speed", label: i18n.sort_fastest(), sort: "download_speed", order: "desc" },
	{ key: "ratio", label: i18n.sort_best_ratio(), sort: "ratio", order: "desc" },
];

export function torrentSortChipKey(
	sort: TorrentSortKey,
	order: SortOrder,
): string | null {
	return (
		TORRENT_SORT_CHIPS.find((c) => c.sort === sort && c.order === order)?.key ??
		null
	);
}
