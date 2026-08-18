<script lang="ts">
	import { Activity, LoaderCircle } from "@lucide/svelte";
	import ActivityRow from "./ActivityRow.svelte";
	import ExpandedRowDetail from "./ExpandedRowDetail.svelte";
	import { cn } from "../../lib/cn";
	import type { QueueEntry, HistoryEntry } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { errorText } from "../../lib/api";

	let {
		view,
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
		onResolve,
	}: {
		view: "queue" | "history";
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
		onResolve?: (item: QueueEntry) => void;
	} = $props();

	const COLSPAN = 6;
	// Columns hide on the table's own container width, not the viewport's: at
	// tablet the rail leaves it ~630px, where a viewport media query still counted
	// the 88px it no longer has and kept a column that fell outside the box.
	type Col = { label: string; hide?: string; grow?: boolean };
	const HEADERS: Record<"queue" | "history", Col[]> = {
		queue: [
			{ label: i18n.common_status() },
			// `grow` + max-w-0 on the cell: the title absorbs the leftover width and
			// truncates, instead of pushing the table past its container.
			{ label: i18n.common_title(), grow: true },
			{ label: i18n.common_progress() },
			{ label: i18n.activity_speed_eta() },
			{ label: i18n.common_client(), hide: "hidden @3xl:table-cell" },
			{ label: "" },
		],
		history: [
			{ label: i18n.common_status() },
			{ label: i18n.common_title(), grow: true },
			// Least actionable of the five, so it's the one that goes.
			{ label: i18n.common_indexer(), hide: "hidden @3xl:table-cell" },
			{ label: i18n.common_size() },
			{ label: i18n.common_when() },
			{ label: "" },
		],
	};
	let headers = $derived(HEADERS[view]);

	// One row open at a time: two expanded details in a table read as two competing
	// answers to "what am I looking at", and on a short viewport the second one
	// pushes the first off screen.
	let expandedId = $state<number | null>(null);
	function toggle(id: number) {
		expandedId = expandedId === id ? null : id;
	}

	// The sentinel only exists in the history view, so it mounts long after the
	// component does and re-mounts on every queue↔history switch. Keying the
	// effect on the binding re-attaches the observer each time; reading hasMore /
	// loadingMore inside the async callback keeps them out of the dependencies.
	let sentinel = $state<HTMLDivElement | null>(null);
	$effect(() => {
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

<!-- The table is the md-and-up reading; below that ActivityTouchList takes over,
     since six columns don't survive 390px. -->
<div
	class="@container mt-3 hidden overflow-x-auto rounded-lg border border-border bg-bg-elevated md:block"
>
	{#if loading}
		<div class="px-5 py-10 text-center text-sm text-fg-subtle">{i18n.common_loading()}</div>
	{:else if error}
		<div class="px-5 py-10 text-center">
			<p class="text-sm font-semibold text-status-failed">
				Failed to load {view}
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{errorText(error)}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-1.5 px-5 py-12 text-center"
		>
			<Activity size={28} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">
				{view === "queue" ? i18n.activity_queue_quiet() : i18n.common_no_history()}
			</p>
			<p class="text-xs text-fg-muted">
				{view === "queue"
					? i18n.activity_queue_help()
					: i18n.activity_none_completed()}
			</p>
		</div>
	{:else}
		<table class="w-full min-w-[520px] border-collapse text-left">
			<thead
				class="sticky top-0 z-10 bg-surface text-[10px] uppercase tracking-[0.12em] text-fg-faint"
			>
				<tr>
					{#each headers as h, i (i)}
						<th
							scope="col"
							class={cn(
								"px-2 py-2.5 font-medium first:pl-4 last:pr-4",
								h.grow ? "w-full max-w-0" : "w-px whitespace-nowrap",
								h.hide,
							)}
						>
							{h.label}
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.id)}
					<ActivityRow
						item={row}
						{view}
						expanded={expandedId === row.id}
						onToggle={toggle}
						{onResolve}
					/>
					{#if expandedId === row.id}
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
					{i18n.common_loading_more()}
				</div>
			{/if}
		{/if}
	{/if}
</div>
