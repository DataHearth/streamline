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
