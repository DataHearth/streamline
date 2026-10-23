import { MutationCache, QueryClient } from "@tanstack/svelte-query";

// Keys the nav badges read (lib/nav-counts.ts). They are always-mounted
// queries on their own poll — 30 s for the queue, 60 s for imports — so a scan
// you just started, or a title you just added, showed a stale badge for up to a
// minute. Anything that changes them is a mutation, so one cache-wide hook
// refreshes them all rather than every call site remembering to.
const NAV_COUNT_KEYS = [
	["movies", "counts"],
	["series", "counts"],
	["activity", "queue"],
	["imports", "counts"],
] as const;

// Tag a query `meta: SILENT` to keep it out of the global loading bar
// (components/layout/GlobalActivityBar.svelte). For polls the user did not
// trigger and cannot wait on — nav badges, the dashboard's glance panels —
// where a bar reads as a stall rather than as progress.
export const SILENT = { silent: true } as const;

// One cadence for every badge and counter in the nav, so the chrome refreshes
// as a unit rather than each corner of it drifting on its own timer. Pages own
// their own rate — the torrents page polls its list every 2 s and the nav rides
// that same cache entry while you are on it.
export const NAV_POLL_MS = 15_000;

export const queryClient = new QueryClient({
	mutationCache: new MutationCache({
		onSuccess: () => {
			for (const queryKey of NAV_COUNT_KEYS) {
				queryClient.invalidateQueries({ queryKey });
			}
		},
	}),
	defaultOptions: {
		queries: {
			staleTime: 30_000,
			gcTime: 5 * 60_000,
			refetchOnWindowFocus: true,
			retry: 1,
		},
		mutations: {
			retry: 0,
		},
	},
});
