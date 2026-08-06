import type { MediaRequest, RequestCounts, RequestStatus } from "./types";
import { m as i18n } from "./paraglide/messages.js";

// One model for the requests page below lg, shared by the row, the two sheets
// and the requester's grouped list. The desktop accordion reads STATUS_META
// too, so the label and the colour cannot drift between the two layouts.

export type StatusTone = "need" | "ok" | "link" | "fail";

// `word` is the row's trailing word: what this request wants from you, rather
// than a restatement of the status pill. For a reviewer a pending request is
// work ("Decide"); for the person who asked it is a wait ("In review").
export const STATUS_META: Record<
	RequestStatus,
	{ label: string; token: string; word: string; mine: string; tone: StatusTone }
> = {
	pending: {
		label: i18n.common_pending(),
		token: "wanted",
		word: i18n.common_decide(),
		mine: i18n.requests_in_review(),
		tone: "need",
	},
	approved: {
		label: i18n.common_approved(),
		token: "grabbing",
		word: i18n.common_approved(),
		mine: i18n.common_approved(),
		tone: "link",
	},
	denied: {
		label: i18n.common_rejected(),
		token: "failed",
		word: i18n.common_rejected(),
		mine: i18n.common_rejected(),
		tone: "fail",
	},
	available: {
		label: i18n.status_available(),
		token: "available",
		word: i18n.status_available(),
		mine: i18n.requests_ready_to_watch(),
		tone: "ok",
	},
};

export const TONE_CLASS: Record<StatusTone, string> = {
	need: "text-status-wanted",
	ok: "text-status-available",
	link: "text-status-grabbing",
	fail: "text-status-failed",
};

// UI "rejected" maps to the API's "denied". `available` is deliberately not a
// tab: a request that landed in the library is only reachable under All, which
// is how the desktop tabs have always behaved.
export type RequestTab = "pending" | "approved" | "rejected" | "all";
export type RequestKind = "all" | "movies" | "series";

export function tabStatus(t: RequestTab): RequestStatus | null {
	if (t === "rejected") return "denied";
	if (t === "all") return null;
	return t;
}

export function requesterName(r: MediaRequest): string {
	return r.requester.display_name || r.requester.email;
}

export function kindToken(mediaType: MediaRequest["media_type"]): string {
	return mediaType === "tvshow" ? "downloading" : "grabbing";
}

export function kindLabel(mediaType: MediaRequest["media_type"]): string {
	return mediaType === "tvshow" ? i18n.lc_series() : i18n.lc_movie();
}

export function outcomeWord(
	r: MediaRequest,
	reviewer: boolean,
): { text: string; tone: StatusTone } {
	const m = STATUS_META[r.status];
	return { text: reviewer ? m.word : m.mine, tone: m.tone };
}

// Search covers the title and the person who asked — the two things anyone
// arriving at this page already knows. Email is included because a household
// with two Camilles has nothing else to tell them apart.
function matches(r: MediaRequest, q: string): boolean {
	const n = q.trim().toLowerCase();
	if (!n) return true;
	return (
		r.title.toLowerCase().includes(n) ||
		(r.requester.display_name ?? "").toLowerCase().includes(n) ||
		r.requester.email.toLowerCase().includes(n)
	);
}

export function filterRequests(
	all: MediaRequest[],
	f: { tab: RequestTab; kind: RequestKind; query: string },
): MediaRequest[] {
	const st = tabStatus(f.tab);
	return all.filter((r) => {
		if (st && r.status !== st) return false;
		if (f.kind === "movies" && r.media_type !== "movie") return false;
		if (f.kind === "series" && r.media_type !== "tvshow") return false;
		return matches(r, f.query);
	});
}

// The badge on the filter button counts what has been narrowed away from the
// landing state (everything, all media types). The touch list has no status
// control of its own — it opens on the whole list, sectioned — so any status
// other than All is a narrowing the badge has to account for. The search field
// is on screen and speaks for itself, so it is not counted here.
export function activeFilterCount(f: {
	tab: RequestTab;
	kind: RequestKind;
}): number {
	return (f.tab === "all" ? 0 : 1) + (f.kind === "all" ? 0 : 1);
}

export function statusChips(
	counts: RequestCounts,
): { key: RequestTab; label: string; count?: number }[] {
	return [
		{ key: "pending", label: i18n.common_pending(), count: counts.pending },
		{ key: "approved", label: i18n.common_approved(), count: counts.approved },
		{ key: "rejected", label: i18n.common_rejected(), count: counts.denied },
		{ key: "all", label: i18n.common_all() },
	];
}

export const KIND_CHIPS: { key: RequestKind; label: string }[] = [
	{ key: "all", label: i18n.common_all() },
	{ key: "movies", label: i18n.movies_label() },
	{ key: "series", label: i18n.series_label() },
];

const byRecent = (a: MediaRequest, b: MediaRequest) =>
	b.updated_at.localeCompare(a.updated_at);

// Pending against everything already decided. Both lists below lg group on this
// split — the reviewer's because search replaced its status control, the
// requester's because they are checking on something rather than triaging — so
// the two cannot drift apart on what counts as settled.
export function groupReview(all: MediaRequest[]): {
	pending: MediaRequest[];
	decided: MediaRequest[];
} {
	return {
		pending: all.filter((r) => r.status === "pending").sort(byRecent),
		decided: all.filter((r) => r.status !== "pending").sort(byRecent),
	};
}

// The requester's view: the same split, named for someone waiting on an answer
// rather than giving one.
export function groupMine(all: MediaRequest[]): {
	waiting: MediaRequest[];
	done: MediaRequest[];
} {
	const g = groupReview(all);
	return { waiting: g.pending, done: g.decided };
}
