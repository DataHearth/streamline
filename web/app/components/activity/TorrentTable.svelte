<script lang="ts">
	import { Magnet, ChevronUp, ChevronDown } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import TorrentRow from "./TorrentRow.svelte";
	import type { SortOrder, TorrentSortKey } from "../../lib/activity-touch";
	import type { Torrent } from "../../lib/types";

	// The md-and-up reading of the torrent list; below that TorrentTouchList takes
	// over. Sorting is the route's state, not this component's: the headers set it
	// here and the filter sheet's chips set it below lg, so the table and the touch
	// list can never disagree about the order.
	let {
		rows,
		loading,
		error = null,
		canControl = false,
		selectedHash = null,
		sort,
		order,
		onSortChange,
		onOpen,
	}: {
		rows: Torrent[];
		loading: boolean;
		error?: Error | null;
		canControl?: boolean;
		selectedHash?: string | null;
		sort: TorrentSortKey;
		order: SortOrder;
		onSortChange: (sort: TorrentSortKey, order: SortOrder) => void;
		onOpen: (hash: string) => void;
	} = $props();

	// `grow` marks the column that absorbs the leftover width. Paired with
	// `max-w-0` on the cell it lets the name truncate instead of forcing the
	// table past its container and spawning a horizontal scrollbar.
	//
	// `hide` is a container query, not a viewport one: the tablet band leaves this
	// table ~630px whatever the viewport says. Nine columns don't fit that, so
	// Size, Ratio and Seeds/Peers drop out and Down / Up collapse into one stacked
	// cell — everything they carried is in the detail the row already opens.
	const HEADERS: {
		label: string;
		key?: TorrentSortKey;
		numeric?: boolean;
		grow?: boolean;
		hide?: string;
	}[] = [
		{ label: "Status", key: "status" },
		{ label: "Name", key: "name", grow: true },
		{ label: "Progress", key: "progress", numeric: true },
		{ label: "↓ / ↑", hide: "@3xl:hidden" },
		{
			label: "Down",
			key: "download_speed",
			numeric: true,
			hide: "hidden @3xl:table-cell",
		},
		{
			label: "Up",
			key: "upload_speed",
			numeric: true,
			hide: "hidden @3xl:table-cell",
		},
		{ label: "Size", key: "size", numeric: true, hide: "hidden @3xl:table-cell" },
		{ label: "Ratio", key: "ratio", numeric: true, hide: "hidden @4xl:table-cell" },
		{
			label: "Seeds/Peers",
			key: "seeds",
			numeric: true,
			hide: "hidden @4xl:table-cell",
		},
		{ label: "" },
	];

	function toggleSort(key: TorrentSortKey, numeric: boolean) {
		if (sort === key) {
			onSortChange(key, order === "asc" ? "desc" : "asc");
			return;
		}
		// Numbers read best largest-first; names and statuses ascending.
		onSortChange(key, numeric ? "desc" : "asc");
	}
</script>

<div
	class="@container hidden overflow-x-auto rounded-lg border border-border bg-bg-elevated md:block"
>
	{#if loading}
		<table class="w-full min-w-[520px] border-collapse text-left">
			<tbody>
				{#each Array(5) as _, i (i)}
					<tr class="border-b border-border">
						<td class="px-4 py-4" colspan={HEADERS.length}>
							<div class="flex items-center gap-4">
								<div
									class="h-4 w-16 rounded-full bg-surface motion-safe:animate-pulse"
								></div>
								<div
									class="h-4 flex-1 rounded bg-surface motion-safe:animate-pulse"
								></div>
								<div
									class="h-4 w-24 rounded bg-surface motion-safe:animate-pulse"
								></div>
							</div>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	{:else if error}
		<div class="px-5 py-10 text-center">
			<p class="text-sm font-semibold text-status-failed">
				Failed to load torrents
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{error.message || "Unknown error"}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-2 px-5 py-14 text-center"
		>
			<Magnet size={26} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">No torrents yet</p>
			<p class="text-xs text-fg-muted">
				{canControl
					? "Use Add torrent above to paste a magnet link or upload a .torrent file."
					: "Nothing is downloading right now."}
			</p>
		</div>
	{:else}
		<table class="w-full min-w-[520px] border-collapse text-left">
			<thead
				class="sticky top-0 z-10 bg-surface text-[10px] uppercase tracking-[0.12em] text-fg-faint"
			>
				<tr>
					{#each HEADERS as h, i (i)}
						<th
							scope="col"
							aria-sort={h.key && sort === h.key
								? order === "asc"
									? "ascending"
									: "descending"
								: undefined}
							class={cn(
								"px-2 py-2.5 font-medium first:pl-4 last:pr-4",
								// Every other column shrinks to its content so the
								// name column keeps the remainder.
								h.grow ? "w-full max-w-0" : "w-px whitespace-nowrap",
								h.hide,
							)}
						>
							{#if h.key}
								{@const key = h.key}
								<button
									type="button"
									onclick={() => toggleSort(key, h.numeric ?? false)}
									class={cn(
										"-mx-1 inline-flex items-center gap-1 whitespace-nowrap rounded px-1 py-0.5 uppercase tracking-[0.12em] transition hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
										sort === h.key && "text-accent-text",
									)}
								>
									{h.label}
									{#if sort === h.key}
										{#if order === "asc"}
											<ChevronUp size={11} class="shrink-0" aria-hidden="true" />
										{:else}
											<ChevronDown size={11} class="shrink-0" aria-hidden="true" />
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
				{#each rows as t (t.hash)}
					<TorrentRow torrent={t} selected={selectedHash === t.hash} {onOpen} />
				{/each}
			</tbody>
		</table>
	{/if}
</div>
