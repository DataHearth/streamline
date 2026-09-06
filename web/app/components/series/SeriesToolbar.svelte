<script lang="ts" module>
	export type SeriesTab =
		| "all"
		| "continuing"
		| "ended"
		| "upcoming"
		| "missing"
		| "downloading"
		| "importing";
	export type SeriesTypeFilter = "all" | "standard" | "anime" | "daily";
	export type SeriesMonFilter = "all" | "monitored" | "unmonitored";
	export type SeriesSort = "recent" | "title" | "year" | "rating" | "episodes";

	export type SeriesTabCounts = Record<SeriesTab, number>;
	export type SeriesMonCounts = Record<SeriesMonFilter, number>;
</script>

<script lang="ts">
	import {
		Search,
		LayoutGrid,
		List,
		ChevronDown,
		Plus,
		X,
		Eye,
		Tv,
		ListChecks,
		CheckCheck,
		SlidersHorizontal,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import SelectionTopBar from "../shared/SelectionTopBar.svelte";
	import MediaFilterSheet from "../shared/MediaFilterSheet.svelte";
	import DropdownMenu from "../shared/DropdownMenu.svelte";
	import DropdownOption from "../shared/DropdownOption.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type View = "grid" | "list";

	let {
		tab,
		typeFilter,
		mon,
		query,
		sort,
		view,
		counts,
		monCounts,
		selectMode,
		selectedCount,
		visibleCount,
		onTabChange,
		onTypeChange,
		onMonChange,
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
		mon: SeriesMonFilter;
		query: string;
		sort: SeriesSort;
		view: View;
		counts: SeriesTabCounts;
		monCounts: SeriesMonCounts;
		selectMode: boolean;
		selectedCount: number;
		visibleCount: number;
		onTabChange: (t: SeriesTab) => void;
		onTypeChange: (t: SeriesTypeFilter) => void;
		onMonChange: (m: SeriesMonFilter) => void;
		onQueryChange: (q: string) => void;
		onSortChange: (s: SeriesSort) => void;
		onViewChange: (v: View) => void;
		onClearFilters: () => void;
		onSelectModeChange: (v: boolean) => void;
		onSelectAll: () => void;
		onAddSeries: () => void;
	} = $props();

	const allTab: { key: SeriesTab; label: string; tint: string; dot: string } = {
		key: "all",
		label: i18n.common_all(),
		tint: "",
		dot: "",
	};
	const tabs: { key: SeriesTab; label: string; tint: string; dot: string }[] = [
		allTab,
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
		// After missing, because that is the order the pipeline runs in: a gap is
		// found, then something is fetched for it.
		{
			key: "downloading",
			label: i18n.status_downloading(),
			tint: "text-status-downloading",
			dot: "bg-status-downloading",
		},
		{
			key: "importing",
			label: i18n.status_importing(),
			tint: "text-status-importing",
			dot: "bg-status-importing",
		},
	];

	const allType: { key: SeriesTypeFilter; label: string } = {
		key: "all",
		label: i18n.lc_all(),
	};
	const typePills: { key: SeriesTypeFilter; label: string }[] = [
		allType,
		{ key: "standard", label: i18n.lc_standard() },
		{ key: "anime", label: i18n.lc_anime() },
		{ key: "daily", label: i18n.lc_daily() },
	];

	const allMon: { key: SeriesMonFilter; label: string } = {
		key: "all",
		label: i18n.common_all(),
	};
	const monOptions: { key: SeriesMonFilter; label: string }[] = [
		allMon,
		{ key: "monitored", label: i18n.monitor_monitored() },
		{ key: "unmonitored", label: i18n.monitor_unmonitored() },
	];

	const sortOptions: { key: SeriesSort; label: string }[] = [
		{ key: "recent", label: i18n.dash_recently_added() },
		{ key: "title", label: i18n.common_sort_title_az() },
		{ key: "year", label: i18n.sort_year_newest() },
		{ key: "rating", label: i18n.sort_rating_highest() },
		{ key: "episodes", label: i18n.sort_most_episodes() },
	];

	// Three facets and sort share one open-menu variable: two booleans meant
	// switching menus wrote to both {#if} conditions in one tick, and the closing
	// block's outro never completed.
	let facet = $state<"status" | "type" | "mon" | "sort" | null>(null);
	// Anchors for the shared <DropdownMenu>, which places itself against the
	// trigger and treats a press on any other trigger as outside.
	let statusBtn = $state<HTMLButtonElement | null>(null);
	let typeBtn = $state<HTMLButtonElement | null>(null);
	let monBtn = $state<HTMLButtonElement | null>(null);
	let sortBtn = $state<HTMLButtonElement | null>(null);

	let currentTab = $derived(tabs.find((t) => t.key === tab) ?? allTab);
	let currentType = $derived(
		typePills.find((t) => t.key === typeFilter) ?? allType,
	);
	let currentMon = $derived(monOptions.find((o) => o.key === mon) ?? allMon);
	let currentSortLabel = $derived(
		sortOptions.find((o) => o.key === sort)?.label ?? i18n.common_sort_title_az(),
	);

	function selectSort(key: SeriesSort) {
		onSortChange(key);
		facet = null;
	}

	// Dismissal — outside press, Escape, the anchor scrolling out of view — is
	// the shared <DropdownMenu>'s. `facet` being a single value is what keeps
	// the four menus exclusive.

	// ── Phone ────────────────────────────────────────────────────────────────
	// One line below md: status chips scroll, everything else — sort, type,
	// monitoring, layout, select — is in the sheet.
	let sheetOpen = $state(false);
	let selecting = $derived(selectMode || selectedCount > 0);
	let activeFilters = $derived(
		(query ? 1 : 0) +
			(tab !== "all" ? 1 : 0) +
			(typeFilter !== "all" ? 1 : 0) +
			(mon !== "all" ? 1 : 0),
	);

	const phoneChip =
		"inline-flex h-11 shrink-0 items-center gap-2 rounded-full border px-3 text-[12.5px] font-medium transition";
	// Keyed triggers, shared by all four menus so the row has one rhythm.
	const trigger =
		"inline-flex h-11 items-center gap-2 whitespace-nowrap rounded-md border px-3 text-[12.5px] font-medium transition focus:outline-none focus:ring-2 focus:ring-accent-ring lg:h-9";
	const triggerOff =
		"border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg";
	const triggerOn = "border-accent-line bg-accent-soft text-accent-text";
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
				{#if mon !== "all"}
					<span
						class={cn(phoneChip, "border-accent-line bg-accent-soft text-accent-text")}
					>
						{currentMon.label}
						<button
							type="button"
							onclick={() => onMonChange("all")}
							aria-label={i18n.common_reset()}
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
					"relative grid h-11 w-11 shrink-0 place-items-center rounded-lg border transition lg:h-9 lg:w-9",
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
	{sortOptions}
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
							"inline-flex min-h-11 shrink-0 items-center rounded-full border px-3.5 font-mono text-[12.5px] lowercase transition lg:h-9 lg:min-h-0",
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
		<div class="pt-5">
			<div
				class="mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
			>
				{i18n.monitor_monitoring()}
			</div>
			<div class="flex flex-wrap gap-2">
				{#each monOptions as o (o.key)}
					{@const on = mon === o.key}
					<button
						type="button"
						aria-pressed={on}
						onclick={() => onMonChange(o.key)}
						class={cn(
							"inline-flex h-11 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition",
							on
								? "border-accent-line bg-accent-soft text-accent-text"
								: "border-border bg-surface text-fg-muted",
						)}
					>
						{o.label}
						<span class="font-mono text-[10.5px] tabular opacity-70">
							{monCounts[o.key]}
						</span>
					</button>
				{/each}
			</div>
		</div>
	{/snippet}
</MediaFilterSheet>

<!-- md and up: three facet menus rather than a tab strip, a pill strip and two
     forced line breaks. Each trigger is keyed and iconed — a status dot in the
     tab's own colour, a screen for type, an eye for monitoring — so the row
     reads "Status Continuing 9" and one line carries what used to take three.
     Below lg sort still collapses into the sheet the phone uses. -->
<div
	class="lib-toolbar sticky top-16 z-20 hidden flex-wrap items-center gap-2 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex md:gap-2.5 md:px-6"
>
	<div class="relative order-1">
		<button
			bind:this={statusBtn}
			type="button"
			onclick={() => (facet = facet === "status" ? null : "status")}
			aria-haspopup="listbox"
			aria-expanded={facet === "status"}
			aria-label={i18n.series_status()}
			class={cn(trigger, tab !== "all" ? triggerOn : triggerOff)}
		>
			<span
				class={cn(
					"h-2 w-2 rounded-full",
					currentTab.dot || (tab === "all" ? "bg-fg-faint" : "bg-accent"),
				)}
				aria-hidden="true"
			></span>
			<span class="facet-key {tab !== 'all' ? 'text-accent-text/70' : 'text-fg-subtle'}">
				{i18n.filter_status()}
			</span>
			<span class={cn(tab !== "all" ? "text-accent-text" : "text-fg")}>
				{currentTab.label}
			</span>
			<span
				class={cn(
					"font-mono text-[10.5px] tabular",
					tab !== "all" ? "text-accent-text/70" : "text-fg-faint",
				)}
			>
				{counts[tab]}
			</span>
			<ChevronDown
				class={cn("h-3.5 w-3.5 transition", facet === "status" && "rotate-180")}
				aria-hidden="true"
			/>
		</button>
		<DropdownMenu
			open={facet === "status"}
			anchor={statusBtn}
			onClose={() => (facet = null)}
			minWidth="14rem"
			ariaLabel={i18n.series_status()}
		>
			{#each tabs as t (t.key)}
				<DropdownOption
					label={t.label}
					dot={t.dot || "bg-fg-faint"}
					count={counts[t.key]}
					selected={tab === t.key}
					onSelect={() => {
						onTabChange(t.key);
						facet = null;
					}}
				/>
			{/each}
		</DropdownMenu>
	</div>

	<div class="relative order-1">
		<button
			bind:this={typeBtn}
			type="button"
			onclick={() => (facet = facet === "type" ? null : "type")}
			aria-haspopup="listbox"
			aria-expanded={facet === "type"}
			aria-label={i18n.series_type()}
			class={cn(trigger, typeFilter !== "all" ? triggerOn : triggerOff)}
		>
			<Tv class="h-3.5 w-3.5" aria-hidden="true" />
			<span
				class="facet-key {typeFilter !== 'all' ? 'text-accent-text/70' : 'text-fg-subtle'}"
			>
				{i18n.common_type()}
			</span>
			<!-- Type keeps the mono lowercase it has always been drawn in. -->
			<span
				class={cn(
					"font-mono text-[11.5px] lowercase",
					typeFilter !== "all" ? "text-accent-text" : "text-fg",
				)}
			>
				{currentType.label}
			</span>
			<ChevronDown
				class={cn("h-3.5 w-3.5 transition", facet === "type" && "rotate-180")}
				aria-hidden="true"
			/>
		</button>
		<DropdownMenu
			open={facet === "type"}
			anchor={typeBtn}
			onClose={() => (facet = null)}
			minWidth="11rem"
			ariaLabel={i18n.series_type()}
		>
			{#each typePills as t (t.key)}
				<DropdownOption
					label={t.label}
					mono
					selected={typeFilter === t.key}
					onSelect={() => {
						onTypeChange(t.key);
						facet = null;
					}}
				/>
			{/each}
		</DropdownMenu>
	</div>

	<div class="relative order-1">
		<button
			bind:this={monBtn}
			type="button"
			onclick={() => (facet = facet === "mon" ? null : "mon")}
			aria-haspopup="listbox"
			aria-expanded={facet === "mon"}
			aria-label={i18n.monitor_monitoring()}
			class={cn(trigger, mon !== "all" ? triggerOn : triggerOff)}
		>
			<Eye class="h-3.5 w-3.5" aria-hidden="true" />
			<span class="facet-key {mon !== 'all' ? 'text-accent-text/70' : 'text-fg-subtle'}">
				{i18n.monitor_monitoring()}
			</span>
			<span class={cn(mon !== "all" ? "text-accent-text" : "text-fg")}>
				{currentMon.label}
			</span>
			<ChevronDown
				class={cn("h-3.5 w-3.5 transition", facet === "mon" && "rotate-180")}
				aria-hidden="true"
			/>
		</button>
		<DropdownMenu
			open={facet === "mon"}
			anchor={monBtn}
			onClose={() => (facet = null)}
			minWidth="12rem"
			ariaLabel={i18n.monitor_monitoring()}
		>
			{#each monOptions as o (o.key)}
				<DropdownOption
					label={o.label}
					count={monCounts[o.key]}
					selected={mon === o.key}
					onSelect={() => {
						onMonChange(o.key);
						facet = null;
					}}
				/>
			{/each}
		</DropdownMenu>
	</div>

	<!-- No forced break: the row wraps only where it runs out of width. A break
	     pinned to a breakpoint fired at widths that fit one line and read as an
	     arbitrary split. When it does wrap, the facets and the field fill the
	     first line and the action group takes the second, right-aligned by
	     `ml-auto` — 768px of tablet width is 698px of content, which three keyed
	     triggers plus that group overrun. -->
	<div
		class="search-wrap order-2 flex h-11 min-w-[7rem] flex-1 items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent lg:h-9"
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

	<div class="order-3 ml-auto flex items-center gap-2">
		<div class="relative hidden md:block">
			<button
				bind:this={sortBtn}
				type="button"
				onclick={() => (facet = facet === "sort" ? null : "sort")}
				aria-haspopup="listbox"
				aria-expanded={facet === "sort"}
				class="inline-flex h-11 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus:ring-2 focus:ring-accent-ring lg:h-9"
			>
				<!-- The key word is the first thing to go when the line is tight; the
				     value alone still reads as a sort. -->
				<span class="sort-key hidden text-fg-subtle">{i18n.filter_sort()}</span>
				<!-- Fixed width: the labels differ in length, and letting the trigger
				     resize moved every control to its right on each pick. -->
				<span class="text-left text-fg lg:w-[7.5rem]">{currentSortLabel}</span>
				<ChevronDown
					class={cn("h-3.5 w-3.5 transition", facet === "sort" && "rotate-180")}
					aria-hidden="true"
				/>
			</button>
			<DropdownMenu
				open={facet === "sort"}
				anchor={sortBtn}
				onClose={() => (facet = null)}
				align="end"
				minWidth="12rem"
				ariaLabel={i18n.filter_sort()}
			>
				{#each sortOptions as opt (opt.key)}
					<DropdownOption
						label={opt.label}
						selected={sort === opt.key}
						onSelect={() => selectSort(opt.key)}
					/>
				{/each}
			</DropdownMenu>
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
					"grid h-10 w-10 place-items-center rounded-sm transition lg:h-7 lg:w-7",
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
					"grid h-10 w-10 place-items-center rounded-sm transition lg:h-7 lg:w-7",
					view === "list" ? "bg-bg-card text-fg" : "text-fg-subtle hover:text-fg",
				)}
			>
				<List size={14} aria-hidden="true" />
				<span class="sr-only">{i18n.common_list_view()}</span>
			</button>
		</div>

		<!-- Selection is a mode, not a filter: one icon, and only in grid view where
		     the cards' checkboxes stay hidden until hover. Select all joins it whenever the mode is on,
		     at every width from md up — the bulk bar carries one too, but only exists once
		     something has been picked. -->
		{#if view === "grid"}
			{#if selectMode}
				<button
					type="button"
					onclick={onSelectAll}
					disabled={visibleCount === 0}
					class="inline-flex h-11 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:pointer-events-none disabled:opacity-40 lg:h-9"
				>
					<CheckCheck size={14} aria-hidden="true" />
					{i18n.common_select_all()}
					<span class="font-mono text-[10px] tabular text-fg-faint">
						{visibleCount}
					</span>
				</button>
			{/if}
			<button
				type="button"
				onclick={() => onSelectModeChange(!selectMode)}
				aria-pressed={selectMode}
				aria-label={i18n.common_select()}
				title={i18n.common_select()}
				class={cn(
					"grid h-11 w-11 shrink-0 place-items-center rounded-md border transition lg:h-9 lg:w-9",
					selectMode ? triggerOn : triggerOff,
				)}
			>
				<ListChecks size={15} aria-hidden="true" />
			</button>
			{#if selectedCount > 0}
				<span class="font-mono text-[11px] tabular text-accent-text">
					{selectedCount}
				</span>
			{/if}
		{/if}

		<button
			type="button"
			onclick={onAddSeries}
			class="hidden h-11 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:inline-flex lg:h-9"
		>
			<Plus size={14} aria-hidden="true" />
			<span class="add-long hidden">{i18n.action_add_series()}</span>
			<span class="add-short">{i18n.common_add()}</span>
		</button>
	</div>
</div>

<style>
	/* ── Fit, measured against the row and not the window ─────────────────────
	   The breakpoints are the viewport's, and this row is not: the sidebar takes
	   248px of it from the width the sidebar expands at, so `lg` fires with about
	   712px of row to play with. Pinning the field at `lg:w-52` there left the
	   first line short and holed while the action group wrapped below it — the
	   break read as arbitrary because the line it broke was not full. The row is
	   its own container instead. Compact, three facets and four controls make one
	   line from 836px of row up; expanded they need 1102. */
	.lib-toolbar {
		container-type: inline-size;
		container-name: libtoolbar;
	}
	/* The threshold is what the expanded row measures (1102px), not a
	   breakpoint: three keys plus a pinned field cost ~350px over the compact
	   row, so turning them on any earlier just moves the wrap back. */
	@container libtoolbar (min-width: 1120px) {
		.search-wrap {
			flex: none;
			width: 13rem;
		}
		.sort-key {
			display: inline;
		}
	}
	/* The key words go before the row does. "Status", "Type" and "Monitoring" are
	   worth ~148px between them, and the dot, the screen, the eye and the value
	   carry the meaning without them. */
	@container libtoolbar (max-width: 1119.98px) {
		.facet-key {
			display: none;
		}
	}
	/* The verb spells itself out once the row can pay the 40px for it — also a
	   container call, not a breakpoint: at 1100px of window with the sidebar out,
	   "Add series" was the 12px that wrapped the row. */
	@container libtoolbar (min-width: 900px) {
		.add-long {
			display: inline;
		}
		.add-short {
			display: none;
		}
	}
</style>
