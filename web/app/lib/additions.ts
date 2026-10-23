import type { SeriesAddition } from "./types";
import { m as i18n } from "./paraglide/messages.js";

const pad = (n: number) => String(n).padStart(2, "0");
const isRun = (eps: number[]) =>
	eps.every((n, i) => i === 0 || n === (eps[i - 1] ?? n) + 1);

/**
 * Names what an import put in the library, so a rail of arrivals can report the
 * arrival rather than the show it landed in. One episode is worth naming; a
 * contiguous run states its ends, because a count alone leaves the reader
 * working out which episodes those were; a pack — a whole season, or several —
 * reports scope and tally, which is what a pack is.
 */
export function additionLabel(a: SeriesAddition): string {
	const seasons = a.seasons ?? [];
	const eps = a.episodes ?? [];
	// Narrowed by value rather than by length: noUncheckedIndexedAccess types
	// every index access as possibly undefined however the length was tested.
	const first = seasons[0];
	const lastSeason = seasons[seasons.length - 1];
	const firstEp = eps[0];
	const lastEp = eps[eps.length - 1];
	if (first === undefined) return "";
	if (seasons.length === 1 && eps.length === 1 && firstEp !== undefined) {
		const code = `S${pad(first)}E${pad(firstEp)}`;
		return a.episode_title ? `${code} · ${a.episode_title}` : code;
	}
	const packed = a.whole_season || seasons.length > 1;
	if (
		!packed &&
		eps.length > 1 &&
		eps.length === a.count &&
		isRun(eps) &&
		firstEp !== undefined &&
		lastEp !== undefined
	)
		return `S${pad(first)}E${pad(firstEp)}–E${pad(lastEp)}`;
	const scope =
		seasons.length === 1 || lastSeason === undefined
			? `S${pad(first)}`
			: `S${pad(first)}–S${pad(lastSeason)}`;
	const n = a.count || eps.length;
	return `${scope} · ${
		n === 1
			? i18n.added_episodes_one({ count: n })
			: i18n.added_episodes_other({ count: n })
	}`;
}
