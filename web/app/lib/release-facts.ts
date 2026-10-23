// What someone who is not a release-scene regular needs from a search result
// before picking it: which language they will hear, how sharp it is, whether it
// will finish, and which one to take when in doubt.
//
// Language is read off the release name here because the search API does not
// carry it yet. The backend parser should own this eventually, the way it owns
// parsed_resolution, and this module then reads the field instead.

import type { Episode, MediaFile, MediaInfo, SearchResult } from "./types";
import { probeOf, resolutionBucket, resolutionOf } from "./media-info";
import { m as i18n } from "./paraglide/messages.js";

// multi — French dub plus the original track (MULTi, DUAL)
// vf    — French dub only (VFF, VFQ, VFI, VF2, TRUEFRENCH, FRENCH)
// vostfr — original track, French subtitles (VOSTFR, SUBFRENCH)
// vo    — no French tag at all: original track only
export type LangKind = "multi" | "vf" | "vostfr" | "vo";

// A dot, dash or underscore is a word boundary to \b, which is every separator
// a release name uses. SUBFRENCH does not match \bfrench\b (no boundary before
// the f), and VOSTFR does not match \bvf\b, so the order below is for clarity
// rather than correctness.
const MULTI = /\b(multi|dual[ ._-]?audio)\b/i;
const SUB_FR = /\b(vostfr|subfrench|stfr)\b/i;
const DUB_FR = /\b(vff|vfq|vfi|vf2|vf|truefrench|french)\b/i;

export function langOf(title: string): LangKind {
	if (MULTI.test(title)) return "multi";
	if (SUB_FR.test(title)) return "vostfr";
	if (DUB_FR.test(title)) return "vf";
	return "vo";
}

const FR = new Set(["fra", "fre", "fr"]);

// A file already in the library is described by its probe, not by its name —
// the renamer has usually taken the tags away. No probe, no claim.
export function langOfProbe(info: MediaInfo | null): LangKind | undefined {
	const audio = (info?.audio_languages ?? []).map((l) => l.toLowerCase());
	if (audio.length === 0) return undefined;
	const fr = audio.some((l) => FR.has(l));
	const other = audio.some((l) => !FR.has(l) && l !== "und");
	if (fr && other) return "multi";
	if (fr) return "vf";
	const subs = (info?.subtitle_languages ?? []).map((l) => l.toLowerCase());
	return subs.some((l) => FR.has(l)) ? "vostfr" : "vo";
}

export function langLabel(k: LangKind): string {
	switch (k) {
		case "multi":
			return i18n.release_lang_multi();
		case "vf":
			return i18n.release_lang_vf();
		case "vostfr":
			return i18n.release_lang_vostfr();
		case "vo":
			return i18n.release_lang_vo();
	}
}

export function langHelp(k: LangKind): string {
	switch (k) {
		case "multi":
			return i18n.release_lang_multi_help();
		case "vf":
			return i18n.release_lang_vf_help();
		case "vostfr":
			return i18n.release_lang_vostfr_help();
		case "vo":
			return i18n.release_lang_vo_help();
	}
}

// Three tints, outside the status ramp: French with the original, French only,
// original only. Same sand/sky pair the calendar uses for its kinds, which is
// why they carry dark text.
export const LANG_CHIP: Record<LangKind, string> = {
	multi: "bg-fg text-bg-deep",
	vf: "bg-[var(--kind-episode)] text-bg-deep",
	vostfr: "bg-[var(--kind-movie)] text-bg-deep",
	vo: "bg-[var(--kind-movie)] text-bg-deep",
};

// Words people use for sharpness. Not translated: Full HD and 4K are what the
// box says in every locale.
export function qualityWord(r: {
	resolution?: string;
	title?: string;
}): string | undefined {
	const res = (r.resolution ?? "").toLowerCase();
	let word: string | undefined;
	if (res.startsWith("2160") || res === "4k") word = "4K";
	else if (res.startsWith("1080")) word = "Full HD";
	else if (res.startsWith("720")) word = "HD";
	else if (res) word = "SD";
	if (word === "4K" && /\b(hdr|hdr10|dv|dovi)\b/i.test(r.title ?? ""))
		return "4K · HDR";
	return word;
}

// Seeders say how fast a torrent is likely to finish. Same thresholds and
// colours as the Seeders column, which already told an operator this.
export type Speed = "fast" | "ok" | "slow";

export function speedOf(seeders: number): Speed {
	if (seeders >= 50) return "fast";
	if (seeders >= 10) return "ok";
	return "slow";
}

export const SPEED_CLASS: Record<Speed, string> = {
	fast: "text-status-available",
	ok: "text-status-wanted",
	slow: "text-status-failed",
};

export const SPEED_DOT: Record<Speed, string> = {
	fast: "bg-status-available",
	ok: "bg-status-wanted",
	slow: "bg-status-failed",
};

export function speedLabel(s: Speed): string {
	if (s === "fast") return i18n.releases_speed_fast();
	if (s === "ok") return i18n.releases_speed_ok();
	return i18n.releases_speed_slow();
}

// The one to take when in doubt: the best profile score among results the
// profile did not reject and that have enough sources to finish. Nothing
// qualifying means no badge — no advice is better than bad advice. Unscored
// searches (no profile resolves) never recommend.
export const RECOMMEND_MIN_SEEDERS = 10;

export function recommendedOf(rows: SearchResult[]): SearchResult | undefined {
	let best: SearchResult | undefined;
	for (const r of rows) {
		if (r.rejected || r.score === undefined) continue;
		if (r.seeders < RECOMMEND_MIN_SEEDERS) continue;
		if (
			!best ||
			r.score > (best.score ?? 0) ||
			(r.score === best.score && r.seeders > best.seeders)
		)
			best = r;
	}
	return best;
}

// ── what a grab would replace ─────────────────────────────────────────────

export type ExistingFile = {
	name: string;
	lang?: LangKind;
	quality?: string;
	size?: number;
};

function baseName(path: string): string {
	return path.split(/[\\/]/).pop() || path;
}

export function existingFromMediaFile(f: MediaFile): ExistingFile {
	return {
		name: baseName(f.path),
		lang: langOfProbe(probeOf(f)),
		quality: qualityWord({ resolution: resolutionOf(f) }),
		size: f.size,
	};
}

export function existingFromEpisode(ep: Episode): ExistingFile | null {
	if (!(ep.size ?? 0) && !ep.path) return null;
	const info = probeOf(ep);
	return {
		name: ep.path ? baseName(ep.path) : "",
		lang: langOfProbe(info),
		quality: qualityWord({
			resolution: resolutionBucket(info?.width, info?.height) ?? ep.quality,
		}),
		size: ep.size ?? undefined,
	};
}
