<script lang="ts" module>
	export type ActivityView = "queue" | "history" | "torrents";

	// `dot` maps each filter to a real --status-* token (some chip keys like
	// "importing"/"error" have no token of their own — mirror lib/format.pillStatus).
	// Exported because the filter sheet renders the same set below lg.
	export const ACTIVITY_CHIPS: Record<
		ActivityView,
		{ key: string; label: string; dot: string }[]
	> = {
		queue: [
			{ key: "downloading", label: i18n.lc_downloading(), dot: "downloading" },
			{ key: "importing", label: i18n.lc_importing(), dot: "grabbing" },
			{ key: "paused", label: i18n.lc_paused(), dot: "paused" },
			{ key: "error", label: i18n.lc_error(), dot: "failed" },
		],
		history: [
			{ key: "completed", label: i18n.lc_completed(), dot: "available" },
			{ key: "failed", label: i18n.lc_failed(), dot: "failed" },
		],
		torrents: [
			{ key: "downloading", label: i18n.lc_downloading(), dot: "downloading" },
			{ key: "stalled", label: "stalled", dot: "stalled" },
			{ key: "seeding", label: "seeding", dot: "seeding" },
			{ key: "completed", label: i18n.lc_completed(), dot: "completed" },
			{ key: "paused", label: i18n.lc_paused(), dot: "paused" },
		],
	};
</script>

<script lang="ts">
	import type { Snippet } from "svelte";
	import { Magnet, Search, Trash2, X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type View = ActivityView;

	// The controls line. `leading` is where /activity puts its Queue / History
	// switch: one line, not two — the switch is a fixed-width block and the chip
	// strip takes whatever is left, swiping through the rest. Below lg the search
	// field and Clear completed move into the filter sheet — 390px cannot hold a
	// chip strip, a field and two buttons, and the tablet band can't either once
	// the rail takes its 88px.
	let {
		leading,
		view,
		statusFilter,
		search,
		onStatusFilterChange,
		onSearchChange,
		onOpenFilters,
		activeFilters = 0,
		onClearCompleted,
		clearableCount = 0,
		onAddTorrent,
		canAddTorrent = false,
	}: {
		leading?: Snippet;
		view: View;
		statusFilter: string[];
		search: string;
		onStatusFilterChange: (s: string[]) => void;
		onSearchChange: (q: string) => void;
		onOpenFilters: () => void;
		activeFilters?: number;
		onClearCompleted?: () => void;
		clearableCount?: number;
		onAddTorrent?: () => void;
		canAddTorrent?: boolean;
	} = $props();

	let chips = $derived(ACTIVITY_CHIPS[view]);
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
	class="sticky top-16 z-20 -mx-4 mb-1 flex flex-nowrap items-center gap-2 bg-bg-deep/85 px-4 pb-3 pt-3 backdrop-blur-md md:-mx-6 md:flex-wrap md:gap-3 md:px-6"
>
	{@render leading?.()}

	<!-- Status filters stay in the open as toggle pills: the set is small and
	     view-specific, and a popover hid which filter was active. The strip swipes
	     rather than wrapping, so the line height never changes and the switch
	     beside it keeps its width. Clear sits outside the scroller. -->
	<div
		class="inline-flex min-w-0 shrink items-center rounded-md border border-border bg-bg-elevated p-[3px] md:flex-none"
	>
		<div
			use:dragScroll
			class="filter-chips flex min-w-0 items-center gap-0.5 overflow-x-auto overscroll-x-contain [scrollbar-width:none] [touch-action:pan-x] [&::-webkit-scrollbar]:hidden"
			role="group"
			aria-label={i18n.activity_status_filter()}
		>
			{#each chips as c (c.key)}
				{@const on = statusFilter.includes(c.key)}
				<button
					type="button"
					onclick={() => toggleChip(c.key)}
					aria-pressed={on}
					class={cn(
						"inline-flex h-7 shrink-0 items-center gap-1.5 rounded-sm px-2.5 font-mono text-[11px] lowercase transition",
						on ? "bg-accent-soft text-accent-text" : "text-fg-subtle hover:text-fg",
					)}
				>
					<span
						class="h-1.5 w-1.5 shrink-0 rounded-full"
						style:background-color="var(--status-{c.dot})"
					></span>
					{c.label}
				</button>
			{/each}
		</div>
		{#if anyActive}
			<button
				type="button"
				onclick={() => onStatusFilterChange([])}
				aria-label={i18n.activity_clear_status_filters()}
				title={i18n.activity_clear_status_filters()}
				class="ml-0.5 grid h-6 w-6 shrink-0 place-items-center rounded-sm text-fg-faint transition hover:bg-surface hover:text-fg"
			>
				<X size={12} aria-hidden="true" />
			</button>
		{/if}
	</div>

	<!-- Search and sort live behind this below lg — a magnifier, since the status
	     pills above never leave the line. History doesn't get one: its two chips
	     are already in the open and Clear completed sits next to them, which left
	     the sheet holding nothing the line didn't. -->
	{#if view !== "history"}
		<button
			type="button"
			onclick={onOpenFilters}
			aria-haspopup="dialog"
			aria-label={i18n.common_search_and_sort()}
			title={i18n.common_search_and_sort()}
			class={cn(
				"relative ml-auto grid h-9 w-9 shrink-0 place-items-center rounded-md border transition lg:hidden",
				activeFilters > 0
					? "border-accent-line bg-accent-soft text-accent-text"
					: "border-border-strong bg-bg-elevated text-fg-muted hover:text-fg",
			)}
		>
			<Search size={15} aria-hidden="true" />
			{#if activeFilters > 0}
				<span
					class="absolute -right-1 -top-1 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 font-mono text-[9.5px] font-semibold text-fg-on-accent"
				>
					{activeFilters}
				</span>
			{/if}
		</button>
	{/if}

	{#if view === "history" && onClearCompleted}
		<button
			type="button"
			onclick={() => onClearCompleted?.()}
			disabled={clearableCount === 0}
			class="ml-auto inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
		>
			<Trash2 size={14} aria-hidden="true" />
			<span class="md:hidden">Clear{clearableCount > 0 ? ` (${clearableCount})` : ""}</span>
			<span class="hidden md:inline">
				Clear completed{clearableCount > 0 ? ` (${clearableCount})` : ""}
			</span>
		</button>
	{/if}

	<div
		class="search-wrap hidden h-9 w-56 items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent lg:flex"
	>
		<Search class="h-3.5 w-3.5 shrink-0 text-fg-subtle" aria-hidden="true" />
		<input
			type="search"
			value={search}
			oninput={(e) => onSearchChange(e.currentTarget.value)}
			placeholder={view === "torrents"
				? i18n.activity_filter_placeholder()
				: i18n.activity_filter_title_movie()}
			aria-label={i18n.activity_filter()}
			class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint"
		/>
		{#if search}
			<button
				type="button"
				onclick={() => onSearchChange("")}
				aria-label={i18n.common_clear_search()}
				class="grid h-5 w-5 shrink-0 place-items-center rounded text-fg-faint transition hover:text-fg"
			>
				<X size={12} aria-hidden="true" />
			</button>
		{/if}
	</div>

	{#if view === "torrents" && onAddTorrent && canAddTorrent}
		<div class="ml-auto flex items-center gap-2">
			<!-- Below md adding is the pill above the bottom nav, not a button in a
			     line the thumb can't reach. -->
			<button
				type="button"
				onclick={() => onAddTorrent?.()}
				class="hidden h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:inline-flex"
			>
				<Magnet size={14} aria-hidden="true" />
				{i18n.action_add_torrent()}
			</button>
		</div>
	{/if}
</div>

<style>
	/* The edge with more chips behind it fades, so a strip narrowed by the switch
	   reads as swipeable rather than as a clipped row. :global because dragScroll
	   sets data-scroll imperatively — Svelte's CSS analysis can't see it and
	   prunes the scoped rules as unused. */
	:global(.filter-chips[data-scroll="start"]) {
		mask-image: linear-gradient(to right, #000 calc(100% - 18px), transparent);
	}
	:global(.filter-chips[data-scroll="middle"]) {
		mask-image: linear-gradient(
			to right,
			transparent,
			#000 18px,
			#000 calc(100% - 18px),
			transparent
		);
	}
	:global(.filter-chips[data-scroll="end"]) {
		mask-image: linear-gradient(to right, transparent, #000 18px);
	}
</style>
