import type { StatusKind } from "@components/shared/StatusPill.svelte";
import type {
	Episode,
	EpisodeStatus,
	Movie,
	Season,
	TVShow,
} from "./types";
import { m as i18n } from "./paraglide/messages.js";
import { countMissing, countWanted } from "./nouns";

// The backend keeps a fileless movie in "wanted" whether or not anyone is
// looking for it. Unmonitored means nobody is, so the card reads "missing".
export function movieStatus(m: Movie): StatusKind {
	if (m.status === "wanted" && !m.monitored) return "missing";
	return m.status;
}

// What a series card reads, rolled up from the show's episode counts.
export function seriesStatus(s: TVShow): StatusKind {
	// A grab in flight outranks the gaps behind it: the card's next change of
	// state is the download landing, not the episode being wanted — and an
	// in-flight episode is counted wanted until its file exists.
	if ((s.downloading_episodes ?? 0) > 0) return "downloading";
	// Past the grab, being written into the library.
	if ((s.importing_episodes ?? 0) > 0) return "importing";
	if ((s.wanted_episodes ?? 0) > 0) return "wanted";
	// Unmonitored shows report zero wanted episodes, so "nothing wanted" alone
	// would badge an empty library entry as available.
	return (s.have_episodes ?? 0) > 0 ? "available" : "missing";
}

export type EpisodeDisplayStatus = EpisodeStatus | "missing";

// Same split as movieStatus. Unaired wins over monitoring: an episode that
// hasn't aired isn't missing, nobody could have it yet.
//
// showMonitored gates the episode's own flag the way db.monitoredEpisode does
// server-side. Unmonitoring a show never wrote through to its episodes, so an
// episode row under one still carries monitored=true; reading that alone made
// this page count gaps the list page had already stopped counting.
export function episodeStatus(
	e: Episode,
	showMonitored = true,
): EpisodeDisplayStatus {
	if (e.status === "wanted" && (!e.monitored || !showMonitored)) {
		return "missing";
	}
	return e.status;
}

export type SeasonTally = { wanted: number; downloading: number; importing: number };

// The tallies a season line names, in the library list's terms: in flight is
// named for what it is, downloading or importing, beside what is still wanted.
// season.missing is every aired, followed episode without a file, in-flight
// grabs included, so a season whose pack was downloading read "22 wanted" in
// the wanted colour above 22 rows saying downloading. With the episodes loaded
// the in-flight ones are split out of wanted; the rollup-only fallback cannot
// tell them apart and keeps the API's count.
export function seasonTallies(s: Season, showMonitored = true): SeasonTally {
	const eps = s.episodes ?? [];
	if (eps.length === 0) return { wanted: s.missing ?? 0, downloading: 0, importing: 0 };
	const t: SeasonTally = { wanted: 0, downloading: 0, importing: 0 };
	for (const e of eps) {
		const st = episodeStatus(e, showMonitored);
		if (st === "wanted") t.wanted++;
		else if (st === "downloading") t.downloading++;
		else if (st === "importing") t.importing++;
	}
	return t;
}

// The one state a season row names beside its count, with the library cards'
// precedence: in flight beats the gap it fills, a wanted gap beats one nobody
// follows. The bar under it carries the rest of the breakdown, so a row whose
// pack is landing reads "20 downloading", not four tallies in four colours.
export function seasonHeadline(
	t: SeasonTally,
	missing: number,
): { text: string; cls: string } | null {
	if (t.downloading > 0)
		return {
			text: `${t.downloading} ${i18n.status_downloading().toLowerCase()}`,
			cls: "text-status-downloading",
		};
	if (t.importing > 0)
		return {
			text: `${t.importing} ${i18n.status_importing().toLowerCase()}`,
			cls: "text-status-importing",
		};
	if (t.wanted > 0) return { text: countWanted(t.wanted), cls: "text-status-wanted" };
	if (missing > 0) return { text: countMissing(missing), cls: "text-status-missing" };
	return null;
}

export function sumTallies(ts: SeasonTally[]): SeasonTally {
	return ts.reduce(
		(a, t) => ({
			wanted: a.wanted + t.wanted,
			downloading: a.downloading + t.downloading,
			importing: a.importing + t.importing,
		}),
		{ wanted: 0, downloading: 0, importing: 0 },
	);
}

export function missingEpisodes(episodes: Episode[], showMonitored = true) {
	return episodes.filter((e) => episodeStatus(e, showMonitored) === "missing")
		.length;
}

// What an episode row is called. A blank title is not "TBA" once the episode
// has an air date or a file: the provider's translation for the configured
// language trails the original airing by weeks, and a downloaded season read
// as a column of "TBA" that looked unaired. Only a dateless placeholder with
// nothing on disk is still to be announced.
export function episodeTitle(e: Episode): string {
	if (e.title) return e.title;
	return e.air_date || e.has_file
		? i18n.episode_number({ number: e.number })
		: i18n.episode_title_tba();
}
