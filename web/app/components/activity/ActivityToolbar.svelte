<script lang="ts">
	import { Search, X, Trash2, Magnet } from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	type View = "queue" | "history" | "torrents";

	let {
		view,
		statusFilter,
		search,
		counts,
		onViewChange,
		onStatusFilterChange,
		onSearchChange,
		onClearCompleted,
		clearableCount = 0,
		onAddTorrent,
		canAddTorrent = false,
	}: {
		view: View;
		statusFilter: string[];
		search: string;
		// Queue and History are two readings of one page, switched here; the
		// torrents route fixes its view and passes neither.
		counts?: { queue: number; history: number };
		onViewChange?: (v: View) => void;
		onStatusFilterChange: (s: string[]) => void;
		onSearchChange: (q: string) => void;
		onClearCompleted?: () => void;
		clearableCount?: number;
		onAddTorrent?: () => void;
		canAddTorrent?: boolean;
	} = $props();

	// `dot` maps each filter to a real --status-* token (some chip keys like
	// "importing"/"error" have no token of their own — mirror lib/format.pillStatus).
	const CHIPS: Record<View, { key: string; label: string; dot: string }[]> = {
		queue: [
			{ key: "downloading", label: "downloading", dot: "downloading" },
			{ key: "importing", label: "importing", dot: "grabbing" },
			{ key: "paused", label: "paused", dot: "paused" },
			{ key: "error", label: "error", dot: "failed" },
		],
		history: [
			{ key: "completed", label: "completed", dot: "available" },
			{ key: "failed", label: "failed", dot: "failed" },
		],
		torrents: [
			{ key: "downloading", label: "downloading", dot: "downloading" },
			{ key: "stalled", label: "stalled", dot: "stalled" },
			{ key: "seeding", label: "seeding", dot: "seeding" },
			{ key: "completed", label: "completed", dot: "completed" },
			{ key: "paused", label: "paused", dot: "paused" },
		],
	};

	const VIEWS: { key: View; label: string }[] = [
		{ key: "queue", label: "Queue" },
		{ key: "history", label: "History" },
	];

	let chips = $derived(CHIPS[view]);
	let anyActive = $derived(chips.some((c) => statusFilter.includes(c.key)));
	function toggleChip(key: string) {
		onStatusFilterChange(
			statusFilter.includes(key)
				? statusFilter.filter((s) => s !== key)
				: [...statusFilter, key],
		);
	}
</script>

<div
	class="sticky top-16 z-20 -mx-4 mb-4 flex flex-col gap-3 bg-bg-deep/85 px-4 pb-3 pt-3 backdrop-blur-md md:-mx-6 md:px-6 lg:-mx-8 lg:px-8"
>
	{#if onViewChange}
		<div class="flex flex-wrap items-center gap-2 md:gap-3">
			<div
				class="inline-flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
				role="group"
				aria-label="List"
			>
				{#each VIEWS as v (v.key)}
					<button
						type="button"
						onclick={() => onViewChange?.(v.key)}
						aria-pressed={view === v.key}
						class={cn(
							"inline-flex shrink-0 items-center gap-1.5 rounded-sm px-3 py-1 text-[12.5px] font-medium transition",
							view === v.key
								? "bg-accent-soft text-accent-text"
								: "text-fg-subtle hover:text-fg",
						)}
					>
						{v.label}
						<span class="font-mono text-[10.5px] tabular text-fg-faint">
							{v.key === "history"
								? (counts?.history ?? 0)
								: (counts?.queue ?? 0)}
						</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<div class="flex flex-wrap items-center gap-2 md:gap-3">
		<!-- Status filters stay in the open as toggle pills: the set is small and
		     view-specific, and a popover hid which filter was active. -->
		<div
			class="inline-flex max-w-full shrink-0 items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-bg-elevated p-[3px] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
			role="group"
			aria-label="Status filter"
		>
			{#each chips as c (c.key)}
				{@const on = statusFilter.includes(c.key)}
				<button
					type="button"
					onclick={() => toggleChip(c.key)}
					aria-pressed={on}
					class={cn(
						"inline-flex shrink-0 items-center gap-1.5 rounded-sm px-2.5 py-1 font-mono text-[11px] lowercase transition",
						on
							? "bg-accent-soft text-accent-text"
							: "text-fg-subtle hover:text-fg",
					)}
				>
					<span
						class="h-1.5 w-1.5 shrink-0 rounded-full"
						style:background-color="var(--status-{c.dot})"
					></span>
					{c.label}
				</button>
			{/each}
			{#if anyActive}
				<button
					type="button"
					onclick={() => onStatusFilterChange([])}
					aria-label="Clear status filters"
					title="Clear status filters"
					class="grid h-6 w-6 shrink-0 place-items-center rounded-sm text-fg-faint transition hover:bg-surface hover:text-fg"
				>
					<X size={12} aria-hidden="true" />
				</button>
			{/if}
		</div>

		<div
			class="search-wrap flex h-9 w-full items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent sm:w-56"
		>
			<Search class="h-3.5 w-3.5 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={search}
				oninput={(e) => onSearchChange(e.currentTarget.value)}
				placeholder={view === "torrents"
					? "Filter name or hash…"
					: "Filter title or movie…"}
				aria-label="Filter activity"
				class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint"
			/>
			{#if search}
				<button
					type="button"
					onclick={() => onSearchChange("")}
					aria-label="Clear search"
					class="grid h-5 w-5 place-items-center rounded text-fg-faint transition hover:text-fg"
				>
					<X size={12} aria-hidden="true" />
				</button>
			{/if}
		</div>

		{#if (view === "torrents" && onAddTorrent && canAddTorrent) || (view === "history" && onClearCompleted)}
			<div class="order-last ml-auto flex items-center gap-2 sm:order-none">
				{#if view === "torrents" && onAddTorrent && canAddTorrent}
					<button
						type="button"
						onclick={() => onAddTorrent?.()}
						class="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow"
					>
						<Magnet size={14} aria-hidden="true" />
						Add torrent
					</button>
				{/if}
				{#if view === "history" && onClearCompleted}
					<button
						type="button"
						onclick={() => onClearCompleted?.()}
						disabled={clearableCount === 0}
						class="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
					>
						<Trash2 size={14} aria-hidden="true" />
						Clear completed{clearableCount > 0 ? ` (${clearableCount})` : ""}
					</button>
				{/if}
			</div>
		{/if}
	</div>
</div>
