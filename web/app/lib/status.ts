import type { StatusKind } from "../components/shared/StatusPill.svelte";
import type { Movie } from "./types";

// The backend keeps a fileless movie in "wanted" whether or not anyone is
// looking for it. Unmonitored means nobody is, so the card reads "missing".
export function movieStatus(m: Movie): StatusKind {
	if (m.status === "wanted" && !m.monitored) return "missing";
	return m.status;
}
