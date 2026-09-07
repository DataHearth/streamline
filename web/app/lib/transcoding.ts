// What the transcoding queue route needs on top of the job records: how a job
// status reads as a pill, the figures each status brings to a row, and the sort
// the page offers at every width.
//
// The queue has no table, so there are no column headers to sort from — the
// chips in the filter sheet below lg and the select in the header from lg both
// write the same key here. See routes/activity/transcoding.svelte.

import type { StatusKind } from "../components/shared/StatusPill.svelte";
import { ApiError } from "./api";
import { formatBytes, formatEta } from "./format";
import { formatRelative } from "./dates";
import type { TranscodeJob, TranscodeStatus } from "./types";
import { m as i18n } from "./paraglide/messages.js";

// Every job status has a StatusKind of its own, aliased onto an existing
// --status-* hue in tokens.css. Mapping onto the borrowed kinds directly would
// have made a queued job's pill read "Wanted" and a canceled one "Paused" —
// exactly the reason `importing` exists beside `grabbing`.
export const transcodeKind = (s: TranscodeStatus): StatusKind => s;

// The endpoints 409 while `transcoding.enabled` is false, and nothing but a
// config change can clear that — so the page stops polling and explains,
// rather than error-toasting a state the operator chose. Same shape as
// activity-nav's engineDisabled.
export const transcodingDisabled = (e: unknown) =>
	e instanceof ApiError && e.status === 409;

export const isLive = (j: TranscodeJob) =>
	j.status === "running" || j.status === "queued";

export const anyLive = (jobs: TranscodeJob[] | undefined) =>
	Boolean(jobs?.some(isLive));

// file_path is a full library path. The row leads with the basename because
// that is what differs between two files in the same folder; the whole path is
// one tap away in the expanded detail.
export function basename(path: string): string {
	const i = path.lastIndexOf("/");
	return i === -1 ? path : path.slice(i + 1);
}

// A job is always about a library item, so the detail offers a way back to it.
// Episodes resolve to their show — there is no per-episode route.
export function jobHref(j: TranscodeJob): string | null {
	if (j.movie_id) return `/movies/${j.movie_id}`;
	if (j.series_id) return `/series/${j.series_id}`;
	return null;
}

export function bytesSaved(j: TranscodeJob): number {
	if (!j.size_before || !j.size_after) return 0;
	return Math.max(0, j.size_before - j.size_after);
}

// Negative because it is a reduction: "−66%" reads as the file got smaller,
// where "66%" alone could be either direction.
export function savedPercent(j: TranscodeJob): string {
	if (!j.size_before || !j.size_after) return "";
	return `−${Math.round((1 - j.size_after / j.size_before) * 100)}%`;
}

export function totalReclaimed(jobs: TranscodeJob[]): number {
	return jobs.reduce((sum, j) => sum + bytesSaved(j), 0);
}

// How long the encode itself took, for a job that finished.
export function elapsed(j: TranscodeJob): string {
	if (!j.started_at || !j.finished_at) return "";
	const s = (Date.parse(j.finished_at) - Date.parse(j.started_at)) / 1000;
	return formatEta(s);
}

export type JobFigure = {
	// The headline figure: a percentage, a size, a before → after pair.
	value: string;
	// The line under it — always the quieter of the two.
	sub: string;
	// A saved badge, succeeded jobs only.
	saved?: string;
	tone?: string;
};

// One figure block per status, so a row only carries the numbers its state
// actually has. A running job that lost its worker-memory progress to a restart
// says so in words: the bar beside it is already indeterminate, and a blank
// where the percentage was reads as a stall.
export function jobFigure(j: TranscodeJob): JobFigure {
	switch (j.status) {
		case "running": {
			if (j.percent === undefined) {
				return { value: "", sub: i18n.transcode_progress_lost() };
			}
			const eta = formatEta(j.eta_seconds);
			return {
				value: `${j.percent.toFixed(1)}%`,
				sub: [
					eta ? i18n.transcode_eta_left({ eta }) : "",
					j.speed ? `${j.speed}×` : "",
					formatBytes(j.size_before, ""),
				]
					.filter(Boolean)
					.join(" · "),
			};
		}
		case "queued":
			return {
				value: formatBytes(j.size_before),
				sub: i18n.transcode_queued_when({ when: formatRelative(j.created_at) }),
			};
		case "succeeded": {
			const took = elapsed(j);
			return {
				value: `${formatBytes(j.size_before)} → ${formatBytes(j.size_after)}`,
				saved: savedPercent(j),
				sub: [
					i18n.transcode_finished_when({
						when: formatRelative(j.finished_at),
					}),
					took,
				]
					.filter(Boolean)
					.join(" · "),
			};
		}
		case "failed":
			return {
				value: i18n.transcode_attempts({ n: j.attempts }),
				sub: i18n.transcode_failed_when({ when: formatRelative(j.finished_at) }),
				tone: "var(--status-failed)",
			};
		default:
			return {
				value: formatBytes(j.size_before),
				sub: i18n.transcode_canceled_when({
					when: formatRelative(j.finished_at ?? j.created_at),
				}),
			};
	}
}

// ── Sort ──────────────────────────────────────────────────────────────────
// The list is one column of rows, so there is nothing to click a header on.
// Both surfaces write these keys: the select in the page header from lg, the
// filter sheet's chips below it.

export type TranscodeSortKey = "attention" | "newest" | "saved" | "size";

// What needs a person, then what is merely news. Failed outranks succeeded
// because it is the only terminal state with an action on it.
const STATUS_RANK: Record<TranscodeStatus, number> = {
	running: 0,
	queued: 1,
	failed: 2,
	succeeded: 3,
	canceled: 4,
};

const newest = (a: TranscodeJob, b: TranscodeJob) =>
	Date.parse(b.created_at) - Date.parse(a.created_at) || b.id - a.id;

// Every comparison ends on the id: the list refetches every 2 s and two rows
// that tie would otherwise swap places on each poll.
export function sortJobs(
	jobs: TranscodeJob[],
	sort: TranscodeSortKey,
): TranscodeJob[] {
	const out = [...jobs];
	if (sort === "saved") {
		out.sort((a, b) => bytesSaved(b) - bytesSaved(a) || newest(a, b));
	} else if (sort === "size") {
		out.sort(
			(a, b) => (b.size_before ?? 0) - (a.size_before ?? 0) || newest(a, b),
		);
	} else if (sort === "newest") {
		out.sort(newest);
	} else {
		out.sort(
			(a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status] || newest(a, b),
		);
	}
	return out;
}

export const TRANSCODE_SORT_CHIPS: {
	key: TranscodeSortKey;
	label: string;
}[] = [
	{ key: "attention", label: i18n.sort_attention_first() },
	{ key: "newest", label: i18n.transcode_sort_newest() },
	{ key: "saved", label: i18n.transcode_sort_saved() },
	{ key: "size", label: i18n.sort_largest() },
];

// POST /transcoding/scan answers 409 for two different reasons — a scan is
// already running, or the worker cannot run at all (ffmpeg disabled or not
// found). Only the first is "started, just not by you"; the second must not
// latch the button into its scan-started state.
export const scanWorkerUnavailable = (e: unknown) =>
	e instanceof ApiError && e.status === 409 && e.body?.code === "worker_unavailable";
