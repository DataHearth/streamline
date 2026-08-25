// What every surface showing a file's technical detail shares: which value wins,
// and how a raw ffprobe field becomes something a person reads.
//
// The rule is probed-first **per field**, not per file. `media_info` is null
// until a file has been probed (or while probing is off), and a probe can also
// land without a given field — an audio-less remux has no `audio_codec`. So each
// helper takes the probed value when there is one and falls back to the
// filename-derived field otherwise. A file with no `media_info` at all therefore
// renders exactly what it rendered before this feature existed: no empty rows,
// no placeholder dashes.

import type {
	AudioTrack,
	Episode,
	MediaFile,
	MediaInfo,
	SubtitleTrack,
} from "./types";
import { getLocale } from "./paraglide/runtime.js";
import { m as i18n } from "./paraglide/messages.js";

// A probe that carries nothing is the same as no probe. The backend writes the
// row before it has a result in at least one case (a file it could not open), so
// a non-null `media_info` is not on its own evidence of anything.
export function probeOf(src: {
	media_info?: MediaInfo | null;
}): MediaInfo | null {
	const i = src.media_info;
	if (!i) return null;
	const empty =
		!i.container &&
		!i.video_codec &&
		!i.height &&
		!i.duration_seconds &&
		!i.audio_codec &&
		!i.bitrate &&
		!i.audio_tracks?.length &&
		!i.subtitles?.length;
	return empty ? null : i;
}

export function isProbed(src: { media_info?: MediaInfo | null }): boolean {
	return probeOf(src) !== null;
}

// ── single fields ─────────────────────────────────────────────────────────

// ffprobe reports dimensions, not the bucket everyone names a file by. Width
// alone decides it: a 2.39:1 2160p release is 3840×1600, so height would call
// it 1600p. These thresholds must stay identical to the importer's verifier
// (`widthBucket` in internal/importer/verify.go) — a file the backend held as
// 720p and this labelled 1080p is an admin resolving a hold against two
// contradictory numbers. Height is only consulted when there is no width.
export function resolutionBucket(
	width?: number,
	height?: number,
): string | undefined {
	const w = width ?? 0;
	const h = height ?? 0;
	if (w) {
		if (w >= 3200) return "2160p";
		if (w >= 1800) return "1080p";
		if (w >= 1200) return "720p";
		return "480p";
	}
	if (!h) return undefined;
	if (h >= 1700) return "2160p";
	if (h >= 900) return "1080p";
	if (h >= 620) return "720p";
	if (h >= 400) return "480p";
	return `${h}p`;
}

// Labels are human; the values stored in a quality profile's `allowed_codecs`
// are ffprobe's own names, so the mapping lives here and runs one way only.
const CODEC_LABELS: Record<string, string> = {
	h264: "H.264",
	avc: "H.264",
	hevc: "HEVC",
	h265: "HEVC",
	av1: "AV1",
	vp9: "VP9",
	vp8: "VP8",
	mpeg2video: "MPEG-2",
	mpeg4: "MPEG-4",
	vc1: "VC-1",
	// Audio
	aac: "AAC",
	ac3: "AC3",
	eac3: "EAC3",
	dts: "DTS",
	truehd: "TrueHD",
	flac: "FLAC",
	opus: "Opus",
	mp3: "MP3",
	// Subtitles
	subrip: "SRT",
	ass: "ASS",
	ssa: "SSA",
	mov_text: "MP4",
	dvd_subtitle: "VobSub",
	hdmv_pgs_subtitle: "PGS",
	webvtt: "VTT",
};

export function codecLabel(codec?: string): string | undefined {
	if (!codec) return undefined;
	return CODEC_LABELS[codec.toLowerCase()] ?? codec.toUpperCase();
}

// Channel counts are said out loud as layouts. Not translated: 5.1 and 7.1 are
// the same two glyphs in every locale, and Mono/Stereo are the industry's terms
// rather than prose — the same exception the S01E02 template takes.
export function channelLayout(channels?: number): string | undefined {
	if (!channels || channels <= 0) return undefined;
	switch (channels) {
		case 1:
			return "Mono";
		case 2:
			return "Stereo";
		case 6:
			return "5.1";
		case 7:
			return "6.1";
		case 8:
			return "7.1";
		default:
			return `${channels}ch`;
	}
}

// ── languages and tracks ──────────────────────────────────────────────────

// ffprobe reports ISO 639-2/B; Intl.DisplayNames speaks 639-1. Only the codes a
// release in the wild actually carries — anything else falls through to the raw
// code, which is more useful than "Unknown".
const LANG_2: Record<string, string> = {
	eng: "en", fra: "fr", fre: "fr", spa: "es", deu: "de", ger: "de", ita: "it",
	por: "pt", nld: "nl", dut: "nl", jpn: "ja", kor: "ko", zho: "zh", chi: "zh",
	rus: "ru", ara: "ar", hin: "hi", pol: "pl", tur: "tr", swe: "sv", nor: "no",
	dan: "da", fin: "fi", ces: "cs", cze: "cs", ell: "el", gre: "el", heb: "he",
	hun: "hu", tha: "th", ukr: "uk", vie: "vi", ron: "ro", rum: "ro", bul: "bg",
	hrv: "hr", srp: "sr", slk: "sk", cat: "ca", ind: "id", may: "ms", msa: "ms",
};

// The badge form: two letters, uppercase. "und" is what a muxer writes when it
// does not know, so it gets a mark rather than a wrong guess.
export function langShort(code?: string): string {
	if (!code) return "??";
	const c = code.toLowerCase();
	if (c === "und" || c === "unknown") return "??";
	return (LANG_2[c] ?? c).slice(0, 2).toUpperCase();
}

// The written-out form, in the reader's own locale — the expanded list has room
// for it and "fra" is not a word.
export function langName(code?: string): string {
	const c = (code ?? "").toLowerCase();
	if (!c || c === "und" || c === "unknown") return i18n.lang_unknown();
	const two = LANG_2[c] ?? c;
	try {
		const dn = new Intl.DisplayNames([getLocale()], { type: "language" });
		return dn.of(two) ?? two.toUpperCase();
	} catch {
		return two.toUpperCase();
	}
}

// Default track first: it is the one that plays, and the one the flat
// audio_codec/audio_channels fields describe.
export function audioTracks(info: MediaInfo | null): AudioTrack[] {
	const tracks = info?.audio_tracks ?? [];
	if (tracks.length < 2) return [...tracks];
	const i = tracks.findIndex((t) => t.default);
	if (i <= 0) return [...tracks];
	return [tracks[i], ...tracks.filter((_, k) => k !== i)];
}

export function subtitleTracks(info: MediaInfo | null): SubtitleTrack[] {
	return info?.subtitles ?? [];
}

export function primaryAudio(info: MediaInfo | null): AudioTrack | undefined {
	return audioTracks(info)[0];
}

// One audio track, written out: "EAC3 · 5.1". Prefers the enumerated default
// track and falls back to the flat fields, per field.
export function audioLabel(info: MediaInfo | null): string | undefined {
	if (!info) return undefined;
	const t = primaryAudio(info);
	const codec = codecLabel(t?.codec ?? info.audio_codec);
	const layout = channelLayout(t?.channels ?? info.audio_channels);
	if (codec && layout) return `${codec} · ${layout}`;
	return codec ?? layout;
}

// A summary plus what it left out. The cap is four: the movie panel is a 320px
// aside, and a remux carrying thirty subtitle tracks would otherwise own the page.
export const TRACK_CAP = 4;

export type TrackSummary = { text: string; hidden: number; total: number };

export function audioSummary(
	info: MediaInfo | null,
	cap = TRACK_CAP,
): TrackSummary | undefined {
	const tracks = audioTracks(info);
	if (tracks.length === 0) {
		const flat = audioLabel(info);
		return flat ? { text: flat, hidden: 0, total: 1 } : undefined;
	}
	if (tracks.length === 1) {
		const t = tracks[0];
		return {
			text: joinDot([
				langShort(t.language),
				codecLabel(t.codec),
				channelLayout(t.channels),
			]),
			hidden: 0,
			total: 1,
		};
	}
	const shown = tracks.slice(0, cap).map((t) =>
		joinSpace([langShort(t.language), channelLayout(t.channels)]),
	);
	return {
		text: shown.join(" · "),
		hidden: Math.max(0, tracks.length - cap),
		total: tracks.length,
	};
}

export function subtitleSummary(
	info: MediaInfo | null,
	cap = TRACK_CAP,
): TrackSummary | undefined {
	const subs = subtitleTracks(info);
	if (subs.length === 0) return undefined;
	if (subs.length === 1) {
		const s = subs[0];
		return {
			text: joinDot([langShort(s.language), codecLabel(s.codec)]),
			hidden: 0,
			total: 1,
		};
	}
	// Languages, deduplicated: a track list of eng/eng-forced/eng-SDH is one
	// language as far as a summary is concerned.
	const langs: string[] = [];
	for (const s of subs) {
		const l = langShort(s.language);
		if (!langs.includes(l)) langs.push(l);
	}
	return {
		text: langs.slice(0, cap).join(", "),
		hidden: Math.max(0, langs.length - cap),
		total: subs.length,
	};
}

// The flags that change what a subtitle track is for.
export function subtitleFlags(s: SubtitleTrack): string[] {
	const out: string[] = [];
	if (s.forced) out.push(i18n.sub_forced());
	if (s.hearing_impaired) out.push(i18n.sub_sdh());
	return out;
}

export function formatBitrate(bps?: number): string | undefined {
	if (!bps || bps <= 0) return undefined;
	if (bps >= 1_000_000) {
		const v = bps / 1_000_000;
		return `${v >= 10 ? Math.round(v) : v.toFixed(1)} Mbps`;
	}
	return `${Math.round(bps / 1000)} kbps`;
}

// The file's own length, which is a different number from the runtime TMDB
// reports for the title — and a disagreement between them is exactly what
// `library.probe.min_duration_ratio` checks.
export function formatDuration(seconds?: number): string | undefined {
	if (!seconds || seconds <= 0) return undefined;
	const total = Math.round(seconds);
	let h = Math.floor(total / 3600);
	// Rounding the remainder can reach 60 — anything in the last half-minute of
	// an hour — and "1h 60m" is not a duration.
	let m = Math.round((total % 3600) / 60);
	if (m === 60) {
		h += 1;
		m = 0;
	}
	if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
	if (m > 0) return `${m}m`;
	return `${total}s`;
}

// ── composed lines ────────────────────────────────────────────────────────

const joinDot = (parts: (string | undefined | null)[]) =>
	parts.filter((p): p is string => Boolean(p && p.trim())).join(" · ");
const joinSpace = (parts: (string | undefined | null)[]) =>
	parts.filter((p): p is string => Boolean(p && p.trim())).join(" ");

// The resolution a file should be described by: the probe's bucket, else the
// word the filename claimed.
export function resolutionOf(file: {
	media_info?: MediaInfo | null;
	parsed_resolution?: string;
}): string | undefined {
	const info = probeOf(file);
	return resolutionBucket(info?.width, info?.height) ?? file.parsed_resolution;
}

// x265 in a release name is the encoder, not the codec — but it is the only
// thing a filename offers, so it stays as the fallback.
export function codecOf(file: {
	media_info?: MediaInfo | null;
	parsed_codec?: string;
}): string | undefined {
	const info = probeOf(file);
	return codecLabel(info?.video_codec) ?? file.parsed_codec;
}

// The episode table's Media cell and the accordion row's meta line. Unprobed
// falls back to the profile's quality word alone, which is all the row has ever
// shown — the caller mutes it, since it means something weaker.
export function episodeMedia(ep: Episode): string {
	const info = probeOf(ep);
	if (!info) return ep.quality ?? "";
	return joinDot([
		resolutionBucket(info.width, info.height) ?? ep.quality,
		codecLabel(info.video_codec),
		channelLayout(info.audio_channels),
	]);
}

// The dashboard card's third line. Size first, as today; the last slot goes to
// the most specific thing known, which is the real codec when probed and the
// release's source word when not.
export function fileMetaLine(
	file: MediaFile | undefined,
	size: string,
): string {
	if (!file) return size;
	const info = probeOf(file);
	return joinDot([
		size,
		resolutionOf(file),
		info ? codecLabel(info.video_codec) : file.parsed_source,
	]);
}

// ── quality profiles ──────────────────────────────────────────────────────

// The codecs a profile can restrict itself to. Closed and short on purpose:
// these are the ones a release in the wild actually uses. An empty
// `allowed_codecs` means any codec, which is how every existing profile behaves.
export const VIDEO_CODECS: { value: string; label: string }[] = [
	{ value: "hevc", label: "HEVC" },
	{ value: "av1", label: "AV1" },
	{ value: "h264", label: "H.264" },
	{ value: "vp9", label: "VP9" },
	{ value: "mpeg2video", label: "MPEG-2" },
];
