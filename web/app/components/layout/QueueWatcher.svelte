<script lang="ts">
	// Nothing pushes from the server, and only the queue polls. A grab flips the
	// movie to downloading and an import flips it to available minutes later —
	// both server-side, both invisible to a tab that stays focused, because the
	// default staleTime only reloads on focus. The queue is the one view that
	// already sees those transitions, so when its (id, status) signature moves,
	// re-read everything downstream of it.
	//
	// ponytail: polls the queue the nav already polls (same key, deduped). Swap
	// for SSE/websockets if a live library view ever needs sub-30s freshness.
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { NAV_POLL_MS, SILENT } from "../../lib/query";
	import { auth } from "../../lib/auth.svelte";
	import type { DownloadQueue } from "../../lib/types";

	const qc = useQueryClient();

	const queue = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		meta: SILENT,
		enabled: auth.user !== null,
		retry: false,
		refetchInterval: NAV_POLL_MS,
	}));

	let signature = $derived(
		queue.data?.items
			.map((i) => `${i.id}:${i.status}`)
			.sort()
			.join(",") ?? null,
	);

	let seen: string | null = null;
	$effect(() => {
		const sig = signature;
		if (sig === null || sig === seen) return;
		const first = seen === null;
		seen = sig;
		// The first sample is what the page already rendered against, not a change.
		if (first) return;
		for (const queryKey of [
			["movies"],
			["movie"],
			["series"],
			["activity"],
			["calendar"],
		]) {
			qc.invalidateQueries({ queryKey });
		}
	});
</script>
