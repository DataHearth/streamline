import { formatBytes } from "./format";
import { getLocale } from "./paraglide/runtime.js";
import { m as i18n } from "./paraglide/messages.js";

// GET /stats — the admin stats page (routes/stats). Every figure is computable
// from what the server already stores, so the endpoint needs no new table:
// library facts from media_file and the four media tables, the daily series
// and the funnel from media_event (create_time is indexed alone and with
// type), failure reasons from download_record.failure_reason, requests from
// request, people from user and session. `range` only bounds the event,
// download and request reads; library and storage are always "now".

export type StatsRange = "7d" | "30d" | "90d";
export const STATS_RANGES: StatsRange[] = ["7d", "30d", "90d"];
export const RANGE_DAYS: Record<StatsRange, number> = { "7d": 7, "30d": 30, "90d": 90 };

export type StatsKind = "movie" | "series" | "music" | "books";

// One bucket of a breakdown: how many files, and what they weigh.
export type StatsSlice = { key: string; count: number; bytes: number };

export type StatsMedia = {
	kind: StatsKind;
	// Movies, shows, artists, books.
	titles: number;
	files: number;
	bytes: number;
	// In the medium's own unit: movies, episodes, albums, books.
	wanted: number;
};

export type StatsVolume = { key: string; path: string; used_bytes: number; total_bytes: number };

export type StatsDay = {
	day: string;
	grabbed: number;
	imported: number;
	failed: number;
	requests: number;
};

export type StatsFailure = { reason: string; count: number; last_at: string };
export type StatsIndexer = { name: string; grabs: number; imported: number; failed: number };

export type StatsPerson = {
	id: number;
	name: string;
	email: string;
	role: "admin" | "member" | "request_only";
	requests: number;
	approved: number;
	denied: number;
	available: number;
	last_seen_at: string | null;
};

export type StatsTotals = { grabbed: number; imported: number; failed: number; requests: number };

export type Stats = {
	range: StatsRange;
	generated_at: string;
	library: {
		media: StatsMedia[];
		volumes: StatsVolume[];
		resolution: StatsSlice[];
		video_codec: StatsSlice[];
		hdr: StatsSlice[];
		audio: StatsSlice[];
		// Video files ffprobe has not read yet: the breakdowns above skip them.
		unprobed: number;
		largest: { title: string; href: string; kind: StatsKind; bytes: number; quality: string }[];
	};
	pipeline: {
		daily: StatsDay[];
		funnel: { searched: number; grabbed: number; downloaded: number; imported: number };
		held: number;
		grab_to_import_p50_s: number;
		grab_to_import_p90_s: number;
		failures: StatsFailure[];
		indexers: StatsIndexer[];
		transcode: { succeeded: number; failed: number; rejected: number; saved_bytes: number; busy_s: number };
		drift: { detected: number; confirmed: number };
	};
	users: {
		total: number;
		// Signed in inside the range.
		active: number;
		requests: { total: number; pending: number; approved: number; denied: number; available: number };
		decide_p50_s: number;
		available_p50_s: number;
		people: StatsPerson[];
	};
	// The same totals over the range before this one, for the deltas.
	previous: StatsTotals;
};

// One slice of a stacked meter, already labelled and coloured.
export type MeterSlice = { key: string; label: string; value: number; color: string; detail?: string };

export type Delta = { text: string; tone: "good" | "bad" | "flat" };

const locale = getLocale();
const pctFmt = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 });
const pctFmt1 = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 1 });

export const share = (v: number, of: number) => (of > 0 ? Math.min(100, (v / of) * 100) : 0);

// Under one percent keeps a decimal, so a rare failure does not read as 0%.
export function fmtPct(v: number, of: number): string {
	if (of <= 0) return "—";
	const r = v / of;
	return r > 0 && r < 0.01 ? pctFmt1.format(r) : pctFmt.format(r);
}

export const fmtCount = (n: number) => n.toLocaleString(locale);
export const fmtSize = (n: number) => formatBytes(n, "0 B");

// A wait, in the two largest units: "38m", "6h 12m", "2d 4h".
export function fmtWait(seconds: number): string {
	if (!seconds || seconds <= 0) return "—";
	const m = Math.round(seconds / 60);
	if (m < 60) return `${Math.max(1, m)}m`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h ${String(m % 60).padStart(2, "0")}m`;
	return `${Math.floor(h / 24)}d ${h % 24}h`;
}

// The change against the previous range, signed with a real minus. A rise is
// good or bad by what is counted: more imports is good, more failures is not.
export function delta(now: number, before: number, rising: "good" | "bad" = "good"): Delta | null {
	if (before <= 0) return null;
	const r = (now - before) / before;
	if (Math.abs(r) < 0.005) return { text: "±0%", tone: "flat" };
	const text = (r > 0 ? "+" : "−") + pctFmt.format(Math.abs(r));
	const up = r > 0;
	return { text, tone: up === (rising === "good") ? "good" : "bad" };
}

export const sumDays = (days: StatsDay[], k: keyof Omit<StatsDay, "day">) =>
	days.reduce((a, d) => a + d[k], 0);

export const totals = (s: Stats): StatsTotals => ({
	grabbed: sumDays(s.pipeline.daily, "grabbed"),
	imported: sumDays(s.pipeline.daily, "imported"),
	failed: sumDays(s.pipeline.daily, "failed"),
	requests: sumDays(s.pipeline.daily, "requests"),
});

export const RANGE_LABEL: Record<StatsRange, () => string> = {
	"7d": i18n.stats_range_7d,
	"30d": i18n.stats_range_30d,
	"90d": i18n.stats_range_90d,
};

export const KIND_TOKEN: Record<StatsKind, string> = {
	movie: "movie",
	series: "episode",
	music: "album",
	books: "book",
};

export const KIND_LABEL: Record<StatsKind, () => string> = {
	movie: i18n.movies_label,
	series: i18n.settings_series,
	music: i18n.music_label,
	books: i18n.books_label,
};

export const KIND_HREF: Record<StatsKind, string> = {
	movie: "/movies",
	series: "/series",
	music: "/music",
	books: "/books",
};

// Breakdowns of one property (resolution, codec) are ordered, not
// categorical: they share one hue and step down in lightness, so the meter
// reads largest to smallest without asking the eye to tell hues apart. Each
// meter takes a hue of its own, so the four read as four questions and the
// page is not a wall of grey. None is a status or a kind hue: those mean
// something elsewhere on this page.
//
// The steps run from a pastel down to the darkest that still clears 2:1 on
// the card (--bg-elevated), every neighbour at least 0.06 apart in lightness,
// so two adjacent slices never blur, colour-blind included. Chroma is held
// inside sRGB per hue: teal and gold run out of room first.
export type MeterHue = "indigo" | "teal" | "gold" | "rose";
const STEP_L = [0.83, 0.72, 0.635, 0.57, 0.505, 0.44];
const METER_HUE: Record<MeterHue, { h: number; c: number[] }> = {
	indigo: { h: 277, c: [0.08, 0.12, 0.135, 0.13, 0.12, 0.11] },
	teal: { h: 192, c: [0.08, 0.12, 0.105, 0.095, 0.085, 0.074] },
	gold: { h: 88, c: [0.08, 0.12, 0.125, 0.11, 0.1, 0.088] },
	rose: { h: 352, c: [0.08, 0.12, 0.135, 0.13, 0.12, 0.11] },
};
export const ramp = (hue: MeterHue, i: number) => {
	const k = Math.min(i, STEP_L.length - 1);
	return `oklch(${STEP_L[k]} ${METER_HUE[hue].c[k]} ${METER_HUE[hue].h})`;
};
// "Other" and "unknown" are no format at all: they stay grey and leave the
// hue's steps to the named slices.
const UNSORTED = new Set(["other", "unknown"]);
const UNSORTED_COLOR = "color-mix(in srgb, var(--fg) 30%, transparent)";

const SLICE_LABEL: Record<string, () => string> = {
	"2160p": () => "2160p",
	"1080p": () => "1080p",
	"720p": () => "720p",
	sd: () => "SD",
	hevc: () => "HEVC",
	h264: () => "H.264",
	av1: () => "AV1",
	dv: () => "Dolby Vision",
	hdr10: () => "HDR10",
	sdr: () => "SDR",
	truehd: () => "TrueHD",
	dts: () => "DTS-HD",
	eac3: () => "E-AC-3",
	ac3: () => "AC-3",
	aac: () => "AAC",
	flac: () => "FLAC",
	other: i18n.stats_other,
	unknown: i18n.common_unknown,
};
export const sliceLabel = (key: string) => (SLICE_LABEL[key] ?? (() => key))();

// A breakdown as meter slices, ramped in the order the server sorted them.
export function toMeter(slices: StatsSlice[], hue: MeterHue, by: "count" | "bytes" = "count"): MeterSlice[] {
	let step = 0;
	return slices.map((s) => ({
		key: s.key,
		label: sliceLabel(s.key),
		value: by === "count" ? s.count : s.bytes,
		color: UNSORTED.has(s.key) ? UNSORTED_COLOR : ramp(hue, step++),
		detail: by === "count" ? fmtSize(s.bytes) : fmtCount(s.count),
	}));
}

const REASON_LABEL: Record<string, () => string> = {
	other: i18n.stats_other,
	stalled: i18n.stats_reason_stalled,
	no_space: i18n.stats_reason_no_space,
	verification: i18n.stats_reason_verification,
	unpack: i18n.stats_reason_unpack,
	tracker: i18n.stats_reason_tracker,
	no_match: i18n.stats_reason_no_match,
};
export const reasonLabel = (r: string) => (REASON_LABEL[r] ?? (() => r))();

// The volume's fill: the accent while there is room, the warning hue past 80%,
// failed past 90% — the same two lines Settings → General draws its usage at.
export function volumeTone(v: StatsVolume): string {
	const p = share(v.used_bytes, v.total_bytes);
	if (p >= 90) return "var(--status-failed)";
	if (p >= 80) return "var(--status-warning)";
	return "var(--progress-fill)";
}
