import type { StatusKind } from "../components/shared/StatusPill.svelte";
import type { Episode, EpisodeStatus, Movie, TVShow } from "./types";

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

export function missingEpisodes(episodes: Episode[], showMonitored = true) {
	return episodes.filter((e) => episodeStatus(e, showMonitored) === "missing")
		.length;
}
