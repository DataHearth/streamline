import { QueryClient } from "@tanstack/svelte-query";

export const queryClient = new QueryClient({
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
