<script lang="ts">
	import {
		Search,
		LayoutGrid,
		List,
		ChevronDown,
		Plus,
		X,
		Eye,
		SlidersHorizontal,
	} from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import SelectionControls from "../shared/SelectionControls.svelte";
	import SelectionTopBar from "../shared/SelectionTopBar.svelte";
	import MediaFilterSheet from "../shared/MediaFilterSheet.svelte";
	import type { Density } from "../shared/MediaFilterSheet.svelte";
	import type { MovieCounts } from "../../lib/types";

	type View = "grid" | "list";
	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";

	let {
		tab,
		query,
		sort,
		order,
		view,
		counts,
		monitoredOnly,
		monitoredCount,
		density,
		selectMode,
		selectedCount,
		visibleCount,
		onTabChange,
		onQueryChange,
		onMonitoredChange,
		onSortChange,
		onViewChange,
		onDensityChange,
		onClearFilters,
		onSelectModeChange,
		onSelectAll,
		onAddMovie,
	}: {
		tab: string;
		query: string;
		sort: SortKey;
		order: SortOrder;
		view: View;
		monitoredOnly: boolean;
		monitoredCount: number;
		density: Density;
		selectMode: boolean;
		selectedCount: number;
		visibleCount: number;
		// Only the per-status tallies are shown here; `trend` (from /movies/counts)
		// isn't needed, so accept the client-computed counts without it.
		counts: Omit<MovieCounts, "trend">;
		onTabChange: (t: string) => void;
		onQueryChange: (q: string) => void;
		onMonitoredChange: (v: boolean) => void;
		onSortChange: (s: SortKey, o: SortOrder) => void;
		onViewChange: (v: View) => void;
		onDensityChange: (v: Density) => void;
		onClearFilters: () => void;
		onSelectModeChange: (v: boolean) => void;
		onSelectAll: () => void;
		onAddMovie: () => void;
	} = $props();

	const tabs = [
		{ key: "all", label: "All", tint: "", dot: "" },
		{
			key: "available",
			label: "Available",
			tint: "text-status-available",
			dot: "bg-status-available",
		},
		{
			key: "downloading",
			label: "Downloading",
			tint: "text-status-downloading",
			dot: "bg-status-downloading",
		},
		{
			key: "wanted",
			label: "Wanted",
			tint: "text-status-wanted",
			dot: "bg-status-wanted",
		},
		{
			key: "failed",
			label: "Failed",
			tint: "text-status-failed",
			dot: "bg-status-failed",
		},
	];

	const sortOptions: { key: `${SortKey}-${SortOrder}`; label: string }[] = [
		{ key: "title-asc", label: "Title A→Z" },
		{ key: "title-desc", label: "Title Z→A" },
		{ key: "year-desc", label: "Year newest" },
		{ key: "year-asc", label: "Year oldest" },
	];

	let sortOpen = $state(false);
	let sortRoot = $state<HTMLDivElement | null>(null);

	let currentSortKey = $derived(`${sort}-${order}` as const);
	let currentSortLabel = $derived(
		sortOptions.find((o) => o.key === currentSortKey)?.label ?? "Title A→Z",
	);

	function selectSort(key: string) {
		const [s, o] = key.split("-") as [SortKey, SortOrder];
		onSortChange(s, o);
		sortOpen = false;
	}

	function onDocClick(e: MouseEvent) {
		if (sortRoot && !sortRoot.contains(e.target as Node)) {
			sortOpen = false;
		}
	}

	$effect(() => {
		if (sortOpen) {
			document.addEventListener("mousedown", onDocClick);
			return () => document.removeEventListener("mousedown", onDocClick);
		}
	});

	function tabCount(key: string): number {
		switch (key) {
			case "all":
				return counts.total;
			case "wanted":
				return counts.wanted;
			case "downloading":
				return counts.downloading;
			case "available":
				return counts.available;
			case "failed":
				return counts.failed;
			default:
				return 0;
		}
	}

	// ── Phone ────────────────────────────────────────────────────────────────
	// Below md the two control rows collapse to one: status chips scroll, and
	// everything else lives in the sheet. A selection replaces the line rather
	// than adding to it.
	let sheetOpen = $state(false);
	let selecting = $derived(selectMode || selectedCount > 0);
	let activeFilters = $derived(
		(query ? 1 : 0) + (monitoredOnly ? 1 : 0) + (tab !== "all" ? 1 : 0),
	);

	const phoneChip =
		"inline-flex h-8 shrink-0 items-center gap-2 rounded-full border px-3 text-[12.5px] font-medium transition";
</script>

<div
	class="sticky top-16 z-20 bg-bg-deep/85 backdrop-blur-md md:hidden"
>
	{#if selecting}
		<SelectionTopBar
			count={selectedCount}
			total={visibleCount}
			noun="title"
			onClear={() => onSelectModeChange(false)}
			{onSelectAll}
		/>
	{:else}
		<div class="flex items-center gap-2 px-4 py-2">
			<nav
				use:dragScroll
				aria-label="Movie status"
				class="filter-tabs flex min-w-0 flex-1 items-center gap-2 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
			>
				{#if query}
					<span
						class={cn(phoneChip, "border-accent-line bg-accent-soft text-accent-text")}
					>
						“{query}”
						<button
							type="button"
							onclick={() => onQueryChange("")}
							aria-label="Clear search"
							class="-mr-1 grid h-6 w-6 place-items-center rounded-full text-accent-text"
						>
							<X size={12} aria-hidden="true" />
						</button>
					</span>
				{/if}
				{#each tabs as t (t.key)}
					{@const active = tab === t.key}
					<button
						type="button"
						onclick={() => onTabChange(t.key)}
						aria-current={active ? "page" : undefined}
						class={cn(
							phoneChip,
							active
								? "border-accent-line bg-accent-soft text-accent-text"
								: "border-border bg-surface text-fg-muted",
						)}
					>
						{#if t.dot}
							<span
								class={cn("h-1.5 w-1.5 rounded-full", t.dot)}
								aria-hidden="true"
							></span>
						{/if}
						{t.label}
						<span class="font-mono text-[10.5px] tabular opacity-70">
							{tabCount(t.key)}
						</span>
					</button>
				{/each}
			</nav>

			<button
				type="button"
				onclick={() => (sheetOpen = true)}
				aria-haspopup="dialog"
				aria-expanded={sheetOpen}
				aria-label="Filter and sort"
				class={cn(
					"relative grid h-9 w-9 shrink-0 place-items-center rounded-lg border transition",
					activeFilters > 0
						? "border-accent-line bg-accent-soft text-accent-text"
						: "border-border-strong bg-bg-elevated text-fg-muted",
				)}
			>
				<SlidersHorizontal size={16} aria-hidden="true" />
				{#if activeFilters > 0}
					<span
						class="absolute -right-1 -top-1 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 font-mono text-[9.5px] font-semibold text-fg-on-accent"
					>
						{activeFilters}
					</span>
				{/if}
			</button>
		</div>
	{/if}
</div>

<MediaFilterSheet
	open={sheetOpen}
	onClose={() => (sheetOpen = false)}
	noun="titles"
	{query}
	{onQueryChange}
	{sortOptions}
	sort={currentSortKey}
	onSortChange={selectSort}
	{monitoredOnly}
	{monitoredCount}
	{onMonitoredChange}
	{view}
	{onViewChange}
	{density}
	{onDensityChange}
	onSelectMode={() => onSelectModeChange(true)}
	onReset={onClearFilters}
	activeCount={activeFilters}
/>

<!-- md and up: the status tabs own the first line, everything else the second.
     Below lg the sort control collapses into the same sheet the phone uses — 698px
     of tablet content cannot hold six labelled controls on one line. -->
<div
	class="sticky top-16 z-20 hidden flex-wrap items-center gap-2 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex md:gap-3 md:px-6"
>
	<nav
		use:dragScroll
		aria-label="Movie status"
		class="filter-tabs order-1 flex w-fit max-w-full shrink-0 items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-bg-elevated p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden lg:order-none"
	>
		{#each tabs as t (t.key)}
			{@const active = tab === t.key}
			<button
				type="button"
				onclick={() => onTabChange(t.key)}
				aria-current={active ? "page" : undefined}
				class={cn(
					"inline-flex shrink-0 items-center gap-2 rounded-sm px-3 py-1.5 text-[12.5px] font-medium transition",
					active
						? "bg-bg-card text-fg shadow-[var(--shadow-1)]"
						: "text-fg-muted hover:text-fg",
				)}
			>
				<span>{t.label}</span>
				<span
					class={cn(
						"rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular",
						t.tint || (active ? "text-accent-text" : "text-fg-faint"),
					)}
				>
					{tabCount(t.key)}
				</span>
			</button>
		{/each}
	</nav>

	<div
		class="order-last ml-auto flex items-center gap-2 lg:order-none"
	>
		<button
			type="button"
			onclick={() => (sheetOpen = true)}
			aria-haspopup="dialog"
			aria-expanded={sheetOpen}
			aria-label="Sort and filter"
			title="Sort and filter"
			class={cn(
				"grid h-9 w-9 shrink-0 place-items-center rounded-md border transition lg:hidden",
				activeFilters > 0
					? "border-accent-line bg-accent-soft text-accent-text"
					: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
			)}
		>
			<SlidersHorizontal size={15} aria-hidden="true" />
		</button>

		<div bind:this={sortRoot} class="relative hidden lg:block">
			<button
				type="button"
				onclick={() => (sortOpen = !sortOpen)}
				aria-haspopup="listbox"
				aria-expanded={sortOpen}
				class="inline-flex h-9 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus:ring-2 focus:ring-accent-ring"
			>
				<span class="text-fg-subtle">Sort</span>
				<!-- Fixed width: the labels differ in length, and letting the trigger
				     resize moved every control to its right on each pick. -->
				<span class="w-[5.5rem] text-left text-fg">{currentSortLabel}</span>
				<ChevronDown
					class={cn(
						"h-3.5 w-3.5 transition",
						sortOpen && "rotate-180",
					)}
					aria-hidden="true"
				/>
			</button>
			{#if sortOpen}
				<div
					role="listbox"
					transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
					class="absolute right-0 top-10 z-30 min-w-[12rem] overflow-hidden rounded-md border border-white/[0.08] bg-bg-elevated/95 py-1 shadow-2 backdrop-blur-md"
				>
					{#each sortOptions as opt (opt.key)}
						{@const selected = currentSortKey === opt.key}
						<button
							type="button"
							role="option"
							aria-selected={selected}
							onclick={() => selectSort(opt.key)}
							class={cn(
								"flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition focus:outline-none",
								selected
									? "bg-white/[0.04] text-fg"
									: "text-fg-muted hover:bg-white/[0.04] hover:text-fg",
							)}
						>
							{opt.label}
						</button>
					{/each}
				</div>
			{/if}
		</div>

		<div
			class="inline-flex items-center rounded-md border border-border bg-bg-elevated p-0.5"
			role="group"
			aria-label="View mode"
		>
			<button
				type="button"
				onclick={() => onViewChange("grid")}
				title="Grid view"
				class={cn(
					"grid h-[30px] w-[30px] place-items-center rounded-sm transition",
					view === "grid"
						? "bg-bg-card text-fg"
						: "text-fg-subtle hover:text-fg",
				)}
			>
				<LayoutGrid size={15} aria-hidden="true" />
				<span class="sr-only">Grid view</span>
			</button>
			<button
				type="button"
				onclick={() => onViewChange("list")}
				title="List view"
				class={cn(
					"grid h-7 w-7 place-items-center rounded-sm transition",
					view === "list"
						? "bg-bg-card text-fg"
						: "text-fg-subtle hover:text-fg",
				)}
			>
				<List size={14} aria-hidden="true" />
				<span class="sr-only">List view</span>
			</button>
		</div>

		<button
			type="button"
			onclick={onAddMovie}
			class="hidden h-9 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:inline-flex"
		>
			<Plus size={14} aria-hidden="true" />
			<span class="hidden lg:inline">Add movie</span>
			<span class="lg:hidden">Add</span>
		</button>
	</div>

	<!-- Below lg the field takes the rest of the tabs' line, which puts everything
	     else on the second one — the same two rows whether the tab strip is as wide
	     as Movies' or as wide as Series'. -->
	<div
		class="search-wrap order-2 flex h-9 min-w-[8rem] flex-1 items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent lg:order-none lg:w-56 lg:flex-none"
	>
			<Search class="h-3.5 w-3.5 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={query}
				oninput={(e) => onQueryChange(e.currentTarget.value)}
				placeholder="Filter…"
				class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint"
			/>
			{#if query}
				<button
					type="button"
					onclick={() => onQueryChange("")}
					aria-label="Clear search"
					class="grid h-5 w-5 place-items-center rounded text-fg-faint transition hover:text-fg"
				>
					<X size={12} aria-hidden="true" />
				</button>
			{/if}
		</div>

	<button
		type="button"
		onclick={() => onMonitoredChange(!monitoredOnly)}
		aria-pressed={monitoredOnly}
		class={cn(
			"order-3 inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border px-3 text-[12.5px] font-medium transition lg:order-none",
			monitoredOnly
				? "border-accent bg-accent-soft text-accent-text"
				: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
		)}
	>
			<Eye size={14} aria-hidden="true" />
			Monitored
			<span
				class={cn(
					"rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular",
					monitoredOnly ? "text-accent-text" : "text-fg-faint",
				)}
			>
				{monitoredCount}
			</span>
		</button>

	<!-- List rows carry their own checkboxes (and a select-all in the header), so
	     the toolbar's Select controls would be a second way to do the same thing. -->
	{#if view === "grid"}
		<div class="order-4 flex items-center gap-2 lg:order-none">
			<SelectionControls
				active={selectMode}
				count={selectedCount}
				total={visibleCount}
				onActiveChange={onSelectModeChange}
				{onSelectAll}
			/>
		</div>
	{/if}
</div>
