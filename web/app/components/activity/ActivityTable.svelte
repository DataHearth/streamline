<script lang="ts">
	import { onMount } from "svelte";
	import { Activity, LoaderCircle } from "@lucide/svelte";
	import ActivityRow from "./ActivityRow.svelte";
	import ExpandedRowDetail from "./ExpandedRowDetail.svelte";
	import type { QueueEntry, HistoryEntry } from "../../lib/types";

	let {
		view,
		density,
		rows,
		loading,
		error,
		busyId = null,
		hasMore = false,
		loadingMore = false,
		canControl = false,
		onLoadMore,
		onCancel,
		onPause,
		onResume,
		onRemove,
	}: {
		view: "queue" | "history";
		density: "comfortable" | "compact";
		rows: (QueueEntry | HistoryEntry)[];
		loading: boolean;
		error: Error | null;
		busyId?: number | null;
		hasMore?: boolean;
		loadingMore?: boolean;
		canControl?: boolean;
		onLoadMore: () => void;
		onCancel: (id: number) => void;
		onPause: (id: number) => void;
		onResume: (id: number) => void;
		onRemove: (id: number) => void;
	} = $props();

	const COLSPAN = 6;
	const HEADERS: Record<"queue" | "history", string[]> = {
		queue: ["Status", "Title", "Progress", "Speed / ETA", "Client", ""],
		history: ["Status", "Title", "Indexer", "Size", "When", ""],
	};
	let headers = $derived(HEADERS[view]);

	let expanded = $state(new Map<number, boolean>());
	function toggle(id: number) {
		const next = new Map(expanded);
		next.set(id, !next.get(id));
		expanded = next;
	}

	let sentinel = $state<HTMLDivElement>();
	onMount(() => {
		const el = sentinel;
		if (!el) return;
		const io = new IntersectionObserver((entries) => {
			if (entries[0]?.isIntersecting && hasMore && !loadingMore) {
				onLoadMore();
			}
		});
		io.observe(el);
		return () => io.disconnect();
	});
</script>

<div
	class="overflow-x-auto rounded-lg border border-border bg-bg-elevated"
>
	{#if loading}
		<div class="px-5 py-10 text-center text-sm text-fg-subtle">Loading…</div>
	{:else if error}
		<div class="px-5 py-10 text-center">
			<p class="text-sm font-semibold text-status-failed">
				Failed to load {view}
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{error.message || "Unknown error"}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-1.5 px-5 py-12 text-center"
		>
			<Activity size={28} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">
				{view === "queue" ? "Queue is quiet" : "No history yet"}
			</p>
			<p class="text-xs text-fg-muted">
				{view === "queue"
					? "Active and queued downloads will appear here."
					: "Completed and failed downloads will appear here."}
			</p>
		</div>
	{:else}
		<table class="w-full min-w-[720px] border-collapse text-left">
			<thead
				class="sticky top-0 z-10 bg-surface text-[10px] uppercase tracking-[0.12em] text-fg-faint"
			>
				<tr>
					{#each headers as h, i (i)}
						<th
							scope="col"
							class="px-2 py-2.5 font-medium first:pl-4 last:pr-4"
						>
							{h}
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.id)}
					<ActivityRow
						item={row}
						{view}
						{density}
						expanded={expanded.get(row.id) ?? false}
						onToggle={toggle}
					/>
					{#if expanded.get(row.id)}
						<ExpandedRowDetail
							item={row}
							{view}
							colspan={COLSPAN}
							busy={busyId === row.id}
							{canControl}
							{onCancel}
							{onPause}
							{onResume}
							{onRemove}
						/>
					{/if}
				{/each}
			</tbody>
		</table>
		{#if view === "history"}
			<div bind:this={sentinel} class="h-px w-full"></div>
			{#if loadingMore}
				<div
					class="flex items-center justify-center gap-2 border-t border-border py-3 text-xs text-fg-muted"
				>
					<LoaderCircle
						size={14}
						class="motion-safe:animate-spin"
						aria-hidden="true"
					/>
					Loading more…
				</div>
			{/if}
		{/if}
	{/if}
</div>
