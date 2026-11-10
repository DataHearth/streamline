// The in-app addresses that name a download, in one place.
//
// Activity gives a download record an address and takes a title to filter its
// queue on; every surface that links one (the events feed today) builds the
// link here, so the query a link carries is exactly the one Activity writes
// for the same state. That matters beyond tidiness: when a link to Activity
// from Activity itself is spelt another way than the page writes it, the
// page's rewrite lands before Routify's push and the link's Back entry is lost
// (see `syncRouteQuery`).

import { setDownloadFilter, type DownloadFilter } from "./title-downloads";

export const ROUTE = {
	activity: "/activity",
} as const;

export const link = {
	/**
	 * A download record on Activity with its detail open: on the queue while it
	 * is in flight, in the history once it is done. The page finds which, so
	 * the link names no view.
	 */
	download: (id: number) => `${ROUTE.activity}?download=${id}`,
	/** Activity's queue filtered on a show, a season, an artist or a book series. */
	downloads: (f: DownloadFilter) => {
		const p = new URLSearchParams();
		setDownloadFilter(p, f);
		return `${ROUTE.activity}?${p.toString()}`;
	},
} as const;
