<script lang="ts">
	import { Activity, LoaderCircle } from "@lucide/svelte";
	import TouchRow from "./TouchRow.svelte";
	import { entryHeading, historyMeta, queueMeta } from "../../lib/activity-touch";
	import { pillStatus } from "../../lib/format";
	import type { HistoryEntry, QueueEntry } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { errorText } from "../../lib/api";

	// The table's replacement below md: one surface, hairline rows, and the ring
	// carrying what the Status and Progress columns used to. Everything else the
	// table showed moves into the row's third line or the detail sheet.
	let {
		view,
		rows,
		loading,
		error,
		hasMore = false,
		loadingMore = false,
		onLoadMore,
		onOpen,
		onResolve,
	}: {
		view: "queue" | "history";
		rows: (QueueEntry | HistoryEntry)[];
		loading: boolean;
		error: Error | null;
		hasMore?: boolean;
		loadingMore?: boolean;
		onLoadMore: () => void;
		onOpen: (item: QueueEntry | HistoryEntry) => void;
		onResolve?: (item: QueueEntry) => void;
	} = $props();

	// Same contract as the table's sentinel: it only exists in the history view,
	// so it mounts long after this component and re-mounts on every switch.
	// Keying the effect on the binding re-attaches the observer each time.
	let sentinel = $state<HTMLDivElement | null>(null);
	$effect(() => {
		const el = sentinel;
		if (!el) return;
		const io = new IntersectionObserver((entries) => {
			if (entries[0]?.isIntersecting && hasMore && !loadingMore) onLoadMore();
		});
		io.observe(el);
		return () => io.disconnect();
	});

	// Importing has finished downloading, so its ring is full even though the
	// entry isn't done; ringReading gives it the spinning arc regardless.
	function progressOf(item: QueueEntry | HistoryEntry): number {
		if (view === "history") return 1;
		const q = item as QueueEntry;
		return q.status === "importing" ? 1 : (q.progress ?? 0);
	}
</script>

<div class="mt-3 md:hidden">
	{#if loading}
		<div
			class="rounded-xl border border-border bg-bg-elevated px-5 py-10 text-center text-sm text-fg-subtle"
		>
			{i18n.common_loading()}
		</div>
	{:else if error}
		<div
			class="rounded-xl border border-status-failed/25 bg-bg-elevated px-5 py-9 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				Failed to load {view}
			</p>
			<p class="mt-1 font-mono text-[11px] text-fg-subtle">
				{errorText(error)}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center gap-1.5 rounded-xl border border-border bg-bg-elevated px-5 py-11 text-center"
		>
			<Activity size={26} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-semibold text-fg">
				{view === "queue" ? i18n.activity_queue_quiet() : i18n.common_no_history()}
			</p>
			<p class="max-w-[16rem] text-xs text-fg-muted">
				{view === "queue"
					? i18n.activity_queue_help()
					: i18n.activity_none_completed()}
			</p>
		</div>
	{:else}
		<div class="overflow-hidden rounded-xl border border-border bg-bg-elevated">
			{#each rows as row (row.id)}
				{@const held =
					view === "queue" && (row as QueueEntry).status === "held"}
				<TouchRow
					status={pillStatus(row.status)}
					progress={progressOf(row)}
					title={entryHeading(row)}
					release={row.title}
					meta={view === "queue"
						? queueMeta(row as QueueEntry)
						: historyMeta(row as HistoryEntry)}
					onOpen={() => onOpen(row)}
					onResolve={held && onResolve
						? () => onResolve(row as QueueEntry)
						: undefined}
					resolveLabel={i18n.action_resolve()}
				/>
			{/each}
		</div>
		{#if view === "history"}
			<div bind:this={sentinel} class="h-px w-full"></div>
			{#if loadingMore}
				<div
					class="flex items-center justify-center gap-2 py-3.5 text-xs text-fg-muted"
				>
					<LoaderCircle
						size={14}
						class="motion-safe:animate-spin"
						aria-hidden="true"
					/>
					{i18n.common_loading_more()}
				</div>
			{/if}
		{/if}
	{/if}
</div>
