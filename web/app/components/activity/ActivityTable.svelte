<script lang="ts">
	import { Activity, LoaderCircle, ChevronUp, ChevronDown } from "@lucide/svelte";
	import ActivityRow from "./ActivityRow.svelte";
	import ExpandedRowDetail from "./ExpandedRowDetail.svelte";
	import { cn } from "../../lib/cn";
	import { entryHeading } from "../../lib/activity-touch";
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
	type SortKey =
		| "status"
		| "title"
		| "progress"
		| "speed"
		| "client"
		| "indexer"
		| "size"
		| "when";
	type Col = { label: string; hide?: string; grow?: boolean; sort?: SortKey };
	const HEADERS: Record<"queue" | "history", Col[]> = {
		queue: [
			{ label: i18n.common_status(), sort: "status" },
			// `grow` + max-w-0 on the cell: the title absorbs the leftover width and
			// truncates, instead of pushing the table past its container.
			{ label: i18n.common_title(), grow: true, sort: "title" },
			{ label: i18n.common_progress(), sort: "progress" },
			// The cell reads speed · ETA; speed is the half worth ordering on, and the
			// ETA follows it for anything actually moving.
			{ label: i18n.activity_speed_eta(), sort: "speed" },
			{ label: i18n.common_client(), hide: "hidden @3xl:table-cell", sort: "client" },
			{ label: "" },
		],
		history: [
			{ label: i18n.common_status(), sort: "status" },
			{ label: i18n.common_title(), grow: true, sort: "title" },
			// Least actionable of the five, so it's the one that goes.
			{ label: i18n.common_indexer(), hide: "hidden @3xl:table-cell", sort: "indexer" },
			{ label: i18n.common_size(), sort: "size" },
			{ label: i18n.common_when(), sort: "when" },
			{ label: "" },
		],
	};
	let headers = $derived(HEADERS[view]);

	type Sort = { key: SortKey; dir: "asc" | "desc" };
	// What each view sorts by before anyone touches a header. The queue has none:
	// it arrives in the order the client is working it, which the table has no
	// better guess at. History is served newest-first, and naming that on the
	// header is the difference between an order and an accident.
	const DEFAULT_SORT: Record<"queue" | "history", Sort | null> = {
		queue: null,
		history: { key: "when", dir: "desc" },
	};

	let picked = $state<Sort | null>(null);
	// Status and Title exist in both views and carry across the switch; a Speed
	// sort has no column to point at in history, so it lapses back to that view's
	// default rather than sorting invisibly.
	let current = $derived.by(() => {
		// Read into a const before the closure: narrowing a mutable binding does
		// not survive into a callback, and this file is type-checked in the repo.
		const p = picked;
		if (p && headers.some((h) => h.sort === p.key)) return p;
		return DEFAULT_SORT[view];
	});
	let active = $derived(current?.key ?? null);
	let sortDir = $derived(current?.dir ?? "desc");

	// Status is ranked, not alphabetised: the pill's label is localised, so an
	// alphabetical order would rearrange itself per language. Ascending reads
	// "needs a person first, then the pipeline, then what is finished with".
	const STATUS_RANK: Record<string, number> = {
		held: 0,
		error: 1,
		failed: 1,
		downloading: 2,
		importing: 3,
		paused: 4,
		completed: 5,
	};

	function sortValue(
		row: QueueEntry | HistoryEntry,
		key: SortKey,
	): string | number {
		const q = row as QueueEntry;
		const h = row as HistoryEntry;
		switch (key) {
			case "status":
				return STATUS_RANK[row.status] ?? 9;
			case "title":
				return entryHeading(row).toLowerCase();
			// An import has no percentage of its own — the row draws the bar full, so
			// it sorts full.
			case "progress":
				return q.status === "importing" ? 1 : (q.progress ?? 0);
			// −1 rather than 0: a held or stalled row has no speed at all, and it
			// belongs behind one that is genuinely sitting at zero.
			case "speed":
				return q.status === "held" ? -1 : (q.download_speed ?? -1);
			case "client":
				return (q.download_client ?? "").toLowerCase();
			case "indexer":
				return (h.indexer ?? "").toLowerCase();
			case "size":
				return row.size ?? 0;
			case "when":
				return Date.parse(h.updated_at) || 0;
		}
	}

	let sorted = $derived.by(() => {
		const key = active;
		if (!key) return rows;
		const dir = sortDir === "asc" ? 1 : -1;
		// Array.sort is stable, so rows the column can't separate keep the order the
		// API sent them in.
		return [...rows].sort((a, b) => {
			const va = sortValue(a, key);
			const vb = sortValue(b, key);
			if (typeof va === "string" || typeof vb === "string")
				return String(va).localeCompare(String(vb)) * dir;
			return (va - vb) * dir;
		});
	});

	function sortBy(key?: SortKey) {
		if (!key) return;
		if (active === key) {
			picked = { key, dir: sortDir === "asc" ? "desc" : "asc" };
			return;
		}
		// Text reads A→Z, and Status reads attention-first, which is what its rank
		// ascends through; a number or a date is asked for biggest-or-newest first.
		const down =
			key === "progress" || key === "speed" || key === "size" || key === "when";
		picked = { key, dir: down ? "desc" : "asc" };
	}

	function ariaSort(key: SortKey): "ascending" | "descending" | "none" {
		if (active !== key) return "none";
		return sortDir === "asc" ? "ascending" : "descending";
	}

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
							aria-sort={h.sort ? ariaSort(h.sort) : undefined}
							class={cn(
								"px-2 py-2.5 font-medium first:pl-4 last:pr-4",
								h.grow ? "w-full max-w-0" : "w-px whitespace-nowrap",
								h.hide,
							)}
						>
							{#if h.sort}
								<button
									type="button"
									onclick={() => sortBy(h.sort)}
									class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
								>
									{h.label}
									{#if active === h.sort}
										{#if sortDir === "asc"}
											<ChevronUp size={12} aria-hidden="true" />
										{:else}
											<ChevronDown size={12} aria-hidden="true" />
										{/if}
									{/if}
								</button>
							{:else}
								{h.label}
							{/if}
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each sorted as row (row.id)}
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
