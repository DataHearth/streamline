import type { StatusKind } from "@components/shared/StatusPill.svelte";
import type {
	Episode,
	EpisodeStatus,
	Movie,
	Season,
	TVShow,
} from "./types";
import { m as i18n } from "./paraglide/messages.js";

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

// The season line's tallies. season.missing is every aired, followed episode
// without a file, so a season whose grab is in flight read "22 wanted" in the
// wanted colour while each of its rows said downloading. With the episodes
// loaded the in-flight ones (downloading, importing) are split out; the
// rollup-only fallback cannot tell them apart and keeps the old count.
export function seasonTallies(
	s: Season,
	showMonitored = true,
): { wanted: number; downloading: number } {
	const eps = s.episodes ?? [];
	if (eps.length === 0) return { wanted: s.missing ?? 0, downloading: 0 };
	let wanted = 0;
	let downloading = 0;
	for (const e of eps) {
		const st = episodeStatus(e, showMonitored);
		if (st === "wanted") wanted++;
		else if (st === "downloading" || st === "importing") downloading++;
	}
	return { wanted, downloading };
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
