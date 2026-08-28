<script lang="ts" module>
	export type SeriesTab =
		| "all"
		| "continuing"
		| "ended"
		| "upcoming"
		| "missing";
	export type SeriesTypeFilter = "all" | "standard" | "anime" | "daily";
	export type SeriesSort = "recent" | "title" | "year" | "rating" | "episodes";

	export type SeriesTabCounts = Record<SeriesTab, number>;
</script>

<script lang="ts">
	import {
		Search,
		LayoutGrid,
		List,
		ChevronDown,
		Plus,
		X,
		SlidersHorizontal,
	} from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import SelectionControls from "../shared/SelectionControls.svelte";
	import SelectionTopBar from "../shared/SelectionTopBar.svelte";
	import MediaFilterSheet from "../shared/MediaFilterSheet.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type View = "grid" | "list";

	let {
		tab,
		typeFilter,
		query,
		sort,
		view,
		counts,
		selectMode,
		selectedCount,
		visibleCount,
		onTabChange,
		onTypeChange,
		onQueryChange,
		onSortChange,
		onViewChange,
		onClearFilters,
		onSelectModeChange,
		onSelectAll,
		onAddSeries,
	}: {
		tab: SeriesTab;
		typeFilter: SeriesTypeFilter;
		query: string;
		sort: SeriesSort;
		view: View;
		counts: SeriesTabCounts;
		selectMode: boolean;
		selectedCount: number;
		visibleCount: number;
		onTabChange: (t: SeriesTab) => void;
		onTypeChange: (t: SeriesTypeFilter) => void;
		onQueryChange: (q: string) => void;
		onSortChange: (s: SeriesSort) => void;
		onViewChange: (v: View) => void;
		onClearFilters: () => void;
		onSelectModeChange: (v: boolean) => void;
		onSelectAll: () => void;
		onAddSeries: () => void;
	} = $props();

	const tabs: { key: SeriesTab; label: string; tint: string; dot: string }[] = [
		{ key: "all", label: i18n.common_all(), tint: "", dot: "" },
		{
			key: "continuing",
			label: i18n.series_continuing(),
			tint: "text-status-available",
			dot: "bg-status-available",
		},
		{
			key: "ended",
			label: i18n.series_ended(),
			tint: "text-status-completed",
			dot: "bg-status-completed",
		},
		{
			key: "upcoming",
			label: i18n.series_upcoming(),
			tint: "text-status-fetching",
			dot: "bg-status-fetching",
		},
		{
			key: "missing",
			label: i18n.series_missing_eps(),
			tint: "text-status-wanted",
			dot: "bg-status-wanted",
		},
	];

	const typePills: { key: SeriesTypeFilter; label: string }[] = [
		{ key: "all", label: i18n.lc_all() },
		{ key: "standard", label: i18n.lc_standard() },
		{ key: "anime", label: i18n.lc_anime() },
		{ key: "daily", label: i18n.lc_daily() },
	];

	const sortOptions: { key: SeriesSort; label: string }[] = [
		{ key: "recent", label: i18n.dash_recently_added() },
		{ key: "title", label: i18n.common_sort_title_az() },
		{ key: "year", label: i18n.sort_year_newest() },
		{ key: "rating", label: i18n.sort_rating_highest() },
		{ key: "episodes", label: i18n.sort_most_episodes() },
	];

	let sortOpen = $state(false);
	let sortRoot = $state<HTMLDivElement | null>(null);

	let currentSortLabel = $derived(
		sortOptions.find((o) => o.key === sort)?.label ?? i18n.common_sort_title_az(),
	);

	function selectSort(key: SeriesSort) {
		onSortChange(key);
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

	// ── Phone ────────────────────────────────────────────────────────────────
	// One line below md: status chips scroll, everything else — sort, type,
	// monitored, layout, select — is in the sheet.
	let sheetOpen = $state(false);
	let selecting = $derived(selectMode || selectedCount > 0);
	let activeFilters = $derived(
		(query ? 1 : 0) +
			(tab !== "all" ? 1 : 0) +
			(typeFilter !== "all" ? 1 : 0),
	);

	const phoneChip =
		"inline-flex h-8 shrink-0 items-center gap-2 rounded-full border px-3 text-[12.5px] font-medium transition";
</script>

<div class="sticky top-16 z-20 bg-bg-deep/85 backdrop-blur-md md:hidden">
	{#if selecting}
		<SelectionTopBar
			count={selectedCount}
			total={visibleCount}
			noun="series"
			nounPlural="series"
			onClear={() => onSelectModeChange(false)}
			{onSelectAll}
		/>
	{:else}
		<div class="flex items-center gap-2 px-4 py-2">
			<nav
				use:dragScroll
				aria-label={i18n.series_status()}
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
							aria-label={i18n.common_clear_search()}
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
						<span class="whitespace-nowrap">{t.label}</span>
						<span class="font-mono text-[10.5px] tabular opacity-70">
							{counts[t.key]}
						</span>
					</button>
				{/each}
			</nav>

			<button
				type="button"
				onclick={() => (sheetOpen = true)}
				aria-haspopup="dialog"
				aria-expanded={sheetOpen}
				aria-label={i18n.filter_and_sort()}
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
	noun="series"
	{query}
	{onQueryChange}
	sortOptions={sortOptions}
	{sort}
	onSortChange={(k) => selectSort(k as SeriesSort)}
	{view}
	{onViewChange}
	onSelectMode={() => onSelectModeChange(true)}
	onReset={onClearFilters}
	activeCount={activeFilters}
>
	{#snippet extra()}
		<div class="pt-5">
			<div
				class="mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
			>
				{i18n.common_type()}
			</div>
			<div class="flex flex-wrap gap-2">
				{#each typePills as t (t.key)}
					{@const on = typeFilter === t.key}
					<button
						type="button"
						aria-pressed={on}
						onclick={() => onTypeChange(t.key)}
						class={cn(
							"inline-flex h-9 shrink-0 items-center rounded-full border px-3.5 font-mono text-[12.5px] lowercase transition",
							on
								? "border-accent-line bg-accent-soft text-accent-text"
								: "border-border bg-surface text-fg-muted",
						)}
					>
						{t.label}
					</button>
				{/each}
			</div>
		</div>
	{/snippet}
</MediaFilterSheet>

<!-- md and up: status tabs on the first line, controls on the second. Below lg
     sort and the type filter move into the same sheet the phone uses. -->
<div
	class="sticky top-16 z-20 hidden flex-wrap items-center gap-2 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex md:gap-3 md:px-6"
>
	<nav
		use:dragScroll
		aria-label={i18n.series_status()}
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
				<span class="whitespace-nowrap">{t.label}</span>
				<span
					class={cn(
						"rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular",
						t.tint || (active ? "text-accent-text" : "text-fg-faint"),
					)}
				>
					{counts[t.key]}
				</span>
			</button>
		{/each}
	</nav>

	<div class="order-last ml-auto flex items-center gap-2 lg:order-none">
		<button
			type="button"
			onclick={() => (sheetOpen = true)}
			aria-haspopup="dialog"
			aria-expanded={sheetOpen}
			aria-label={i18n.common_sort_and_filter()}
			title={i18n.common_sort_and_filter()}
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
				<span class="text-fg-subtle">{i18n.filter_sort()}</span>
				<!-- Fixed width: the labels differ in length, and letting the trigger
				     resize moved every control to its right on each pick. -->
				<span class="w-[7.5rem] text-left text-fg">{currentSortLabel}</span>
				<ChevronDown
					class={cn("h-3.5 w-3.5 transition", sortOpen && "rotate-180")}
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
						{@const selected = sort === opt.key}
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
			aria-label={i18n.common_view_mode()}
		>
			<button
				type="button"
				onclick={() => onViewChange("grid")}
				title={i18n.common_grid_view()}
				class={cn(
					"grid h-[30px] w-[30px] place-items-center rounded-sm transition",
					view === "grid" ? "bg-bg-card text-fg" : "text-fg-subtle hover:text-fg",
				)}
			>
				<LayoutGrid size={15} aria-hidden="true" />
				<span class="sr-only">{i18n.common_grid_view()}</span>
			</button>
			<button
				type="button"
				onclick={() => onViewChange("list")}
				title={i18n.common_list_view()}
				class={cn(
					"grid h-7 w-7 place-items-center rounded-sm transition",
					view === "list" ? "bg-bg-card text-fg" : "text-fg-subtle hover:text-fg",
				)}
			>
				<List size={14} aria-hidden="true" />
				<span class="sr-only">{i18n.common_list_view()}</span>
			</button>
		</div>

		<button
			type="button"
			onclick={onAddSeries}
			class="hidden h-9 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:inline-flex"
		>
			<Plus size={14} aria-hidden="true" />
			<span class="hidden lg:inline">{i18n.action_add_series()}</span>
			<span class="lg:hidden">{i18n.common_add()}</span>
		</button>
	</div>

	<!-- Flex line breaks: wrapping alone packs everything onto the first line on a
	     wide screen. These pin the three rows — status tabs, type, then search and
	     the selection controls — at every width from lg up. -->
	<div class="hidden w-full lg:block"></div>

	<div
		class="hidden max-w-full shrink-0 items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-bg-elevated p-[3px] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden lg:inline-flex"
		role="group"
		aria-label={i18n.series_type()}
	>
			{#each typePills as t (t.key)}
				{@const active = typeFilter === t.key}
				<button
					type="button"
					onclick={() => onTypeChange(t.key)}
					aria-pressed={active}
					class={cn(
						"shrink-0 rounded-sm px-2.5 py-1 font-mono text-[11px] lowercase transition",
						active
							? "bg-bg-card text-fg shadow-[var(--shadow-1)]"
							: "text-fg-subtle hover:text-fg",
					)}
				>
					{t.label}
				</button>
			{/each}
		</div>

	<div class="hidden w-full lg:block"></div>

	<!-- Below lg the field takes the rest of the tabs' line, which puts everything
	     else on the second one — the same two rows as Movies, whose tab strip is
	     narrower. -->
	<div
		class="search-wrap order-2 flex h-9 min-w-[8rem] flex-1 items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent lg:order-none lg:w-56 lg:flex-none"
	>
			<Search class="h-3.5 w-3.5 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={query}
				oninput={(e) => onQueryChange(e.currentTarget.value)}
				placeholder={i18n.common_filter_ellipsis()}
				class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint"
			/>
			{#if query}
				<button
					type="button"
					onclick={() => onQueryChange("")}
					aria-label={i18n.common_clear_search()}
					class="grid h-5 w-5 place-items-center rounded text-fg-faint transition hover:text-fg"
				>
					<X size={12} aria-hidden="true" />
				</button>
			{/if}
		</div>

	<div
		class="order-3 flex w-full flex-wrap items-center gap-2 lg:order-none lg:w-auto"
	>
		<!-- List rows carry their own checkboxes (and a select-all in the header), so
		     the toolbar's Select controls would be a second way to do the same thing. -->
		{#if view === "grid"}
			<SelectionControls
				active={selectMode}
				count={selectedCount}
				total={visibleCount}
				onActiveChange={onSelectModeChange}
				{onSelectAll}
			/>
		{/if}
	</div>
</div>
