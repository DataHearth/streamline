import type { StatusKind } from "../components/shared/StatusPill.svelte";
import type { Episode, EpisodeStatus, Movie } from "./types";

// The backend keeps a fileless movie in "wanted" whether or not anyone is
// looking for it. Unmonitored means nobody is, so the card reads "missing".
export function movieStatus(m: Movie): StatusKind {
	if (m.status === "wanted" && !m.monitored) return "missing";
	return m.status;
}

export type EpisodeDisplayStatus = EpisodeStatus | "missing";

// Same split as movieStatus. Unaired wins over monitoring: an episode that
// hasn't aired isn't missing, nobody could have it yet.
export function episodeStatus(e: Episode): EpisodeDisplayStatus {
	if (e.status === "wanted" && !e.monitored) return "missing";
	return e.status;
}

export function missingEpisodes(episodes: Episode[]): number {
	return episodes.filter((e) => episodeStatus(e) === "missing").length;
}
