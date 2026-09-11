<script lang="ts">
	import {
		Search,
		LayoutGrid,
		List,
		ChevronDown,
		Plus,
		X,
		Eye,
		EyeOff,
		Layers,
		ListChecks,
		CheckCheck,
		SlidersHorizontal,
		type LucideIcon,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import SelectionTopBar from "../shared/SelectionTopBar.svelte";
	import MediaFilterSheet from "../shared/MediaFilterSheet.svelte";
	import DropdownMenu from "../shared/DropdownMenu.svelte";
	import DropdownOption from "../shared/DropdownOption.svelte";
	import type { MovieCounts } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type View = "grid" | "list";
	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";

	let {
		tab,
		mon,
		query,
		sort,
		order,
		view,
		counts,
		selectMode,
		selectedCount,
		visibleCount,
		onTabChange,
		onMonChange,
		onQueryChange,
		onSortChange,
		onViewChange,
		onClearFilters,
		onSelectModeChange,
		onSelectAll,
		onAddMovie,
	}: {
		tab: string;
		mon: string;
		query: string;
		sort: SortKey;
		order: SortOrder;
		view: View;
		selectMode: boolean;
		selectedCount: number;
		visibleCount: number;
		// Only the per-status tallies are shown here; `trend` (from /movies/counts)
		// isn't needed, so accept the client-computed counts without it.
		counts: Omit<MovieCounts, "trend">;
		onTabChange: (t: string) => void;
		onMonChange: (m: string) => void;
		onQueryChange: (q: string) => void;
		onSortChange: (s: SortKey, o: SortOrder) => void;
		onViewChange: (v: View) => void;
		onClearFilters: () => void;
		onSelectModeChange: (v: boolean) => void;
		onSelectAll: () => void;
		onAddMovie: () => void;
	} = $props();

	const allTab = { key: "all", label: i18n.common_all(), tint: "", dot: "" };
	const tabs = [
		allTab,
		{
			key: "available",
			label: i18n.status_available(),
			tint: "text-status-available",
			dot: "bg-status-available",
		},
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
		{
			key: "wanted",
			label: i18n.status_wanted(),
			tint: "text-status-wanted",
			dot: "bg-status-wanted",
		},
		{
			key: "failed",
			label: i18n.status_failed(),
			tint: "text-status-failed",
			dot: "bg-status-failed",
		},
	];

	const sortOptions: { key: `${SortKey}-${SortOrder}`; label: string }[] = [
		{ key: "title-asc", label: i18n.common_sort_title_az() },
		{ key: "title-desc", label: i18n.sort_title_za() },
		{ key: "year-desc", label: i18n.sort_year_newest() },
		{ key: "year-asc", label: i18n.sort_year_oldest() },
	];

	// Icons rather than dots: monitoring is a kind, not a pipeline state, and a
	// coloured dot reads as a status everywhere else in this toolbar.
	type MonOption = { key: string; label: string; icon: LucideIcon };
	const allMon: MonOption = {
		key: "all",
		label: i18n.common_all(),
		icon: Layers,
	};
	const monOptions: MonOption[] = [
		allMon,
		{ key: "monitored", label: i18n.monitor_monitored(), icon: Eye },
		{ key: "unmonitored", label: i18n.monitor_unmonitored(), icon: EyeOff },
	];

	// "status" | "mon" | null. Two booleans meant switching menus wrote to both
	// conditions in one tick — the closing block's outro never completed and the
	// menu stayed mounted, dead, over the grid.
	let facet = $state<"status" | "mon" | null>(null);
	let statusOpen = $derived(facet === "status");
	let monOpen = $derived(facet === "mon");
	let sortOpen = $state(false);
	// Anchors for the shared <DropdownMenu>, which places itself against the
	// trigger and treats a press on any other trigger as outside.
	let statusBtn = $state<HTMLButtonElement | null>(null);
	let monBtn = $state<HTMLButtonElement | null>(null);
	let sortBtn = $state<HTMLButtonElement | null>(null);

	let currentTab = $derived(tabs.find((t) => t.key === tab) ?? allTab);
	let currentMon = $derived(monOptions.find((o) => o.key === mon) ?? allMon);

	function pickTab(key: string) {
		onTabChange(key);
		facet = null;
	}
	function pickMon(key: string) {
		onMonChange(key);
		facet = null;
	}
	// Each facet reads its own "all": `total` is the library and would overstate
	// a dropdown whose other facet is filtered.
	function monCount(key: string): number {
		if (key === "monitored") return counts.monitored;
		if (key === "unmonitored") return counts.unmonitored;
		return counts.monitored_total;
	}

	let currentSortKey = $derived(`${sort}-${order}` as const);
	let currentSortLabel = $derived(
		sortOptions.find((o) => o.key === currentSortKey)?.label ?? i18n.common_sort_title_az(),
	);

	function selectSort(key: string) {
		const [s, o] = key.split("-") as [SortKey, SortOrder];
		onSortChange(s, o);
		sortOpen = false;
	}

	$effect(() => {
		if (!facet && !sortOpen) return;
		// Dismissal (outside press, Escape, the anchor scrolling out of view) is the
		// dropdown component's; this only keeps the two conditions exclusive.
		if (facet && sortOpen) sortOpen = false;
	});


	function tabCount(key: string): number {
		switch (key) {
			case "all":
				return counts.status_total;
			case "wanted":
				return counts.wanted;
			case "downloading":
				return counts.downloading;
			case "importing":
				return counts.importing;
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
		(query ? 1 : 0) + (tab !== "all" ? 1 : 0) + (mon !== "all" ? 1 : 0),
	);

	const phoneChip =
		"inline-flex h-11 shrink-0 items-center gap-2 rounded-full border px-3 text-[12.5px] font-medium transition";
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
				aria-label={i18n.movies_status()}
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
				aria-label={i18n.filter_and_sort()}
				class={cn(
					"relative grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-lg border transition",
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
							{monCount(o.key)}
						</span>
					</button>
				{/each}
			</div>
		</div>
	{/snippet}
</MediaFilterSheet>

<!-- md and up: two facet menus rather than a tab strip. Each trigger is keyed
     and iconed — a status dot in the current status' colour, an eye for
     monitoring — so the row reads "Status Wanted 5" without anything having to
     be learned from a glyph alone, and a third facet costs one control instead
     of a second line. Below lg the search field takes its own line and sort
     collapses into the sheet the phone uses. -->
<div
	class="lib-toolbar sticky top-16 z-20 hidden flex-wrap items-center gap-2 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex md:gap-2.5 md:px-6"
>
	<div class="relative order-1">
		<button
			bind:this={statusBtn}
			type="button"
			onclick={() => (facet = statusOpen ? null : "status")}
			aria-haspopup="listbox"
			aria-expanded={statusOpen}
			aria-label={i18n.movies_status()}
			class={cn(
				"inline-flex h-11 items-center gap-2 whitespace-nowrap rounded-md border px-3 text-[12.5px] font-medium transition focus:outline-none focus:ring-2 focus:ring-accent-ring lg:h-9",
				tab !== "all"
					? "border-accent-line bg-accent-soft text-accent-text"
					: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
			)}
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
				{tabCount(tab)}
			</span>
			<ChevronDown
				class={cn("h-3.5 w-3.5 transition", statusOpen && "rotate-180")}
				aria-hidden="true"
			/>
		</button>
		<DropdownMenu
			open={statusOpen}
			anchor={statusBtn}
			onClose={() => (facet = null)}
			minWidth="13rem"
			ariaLabel={i18n.movies_status()}
		>
			{#each tabs as t (t.key)}
				<DropdownOption
					label={t.label}
					dot={t.dot || "bg-fg-faint"}
					count={tabCount(t.key)}
					selected={tab === t.key}
					onSelect={() => pickTab(t.key)}
				/>
			{/each}
		</DropdownMenu>
	</div>

	<div class="relative order-1">
		<button
			bind:this={monBtn}
			type="button"
			onclick={() => (facet = monOpen ? null : "mon")}
			aria-haspopup="listbox"
			aria-expanded={monOpen}
			aria-label={i18n.monitor_monitoring()}
			class={cn(
				"inline-flex h-11 items-center gap-2 whitespace-nowrap rounded-md border px-3 text-[12.5px] font-medium transition focus:outline-none focus:ring-2 focus:ring-accent-ring lg:h-9",
				mon !== "all"
					? "border-accent-line bg-accent-soft text-accent-text"
					: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
			)}
		>
			<Eye class="h-3.5 w-3.5" aria-hidden="true" />
			<span
				class="facet-key {mon !== 'all' ? 'text-accent-text/70' : 'text-fg-subtle'}"
			>
				{i18n.monitor_monitoring()}
			</span>
			<span class={cn(mon !== "all" ? "text-accent-text" : "text-fg")}>
				{currentMon.label}
			</span>
			<ChevronDown
				class={cn("h-3.5 w-3.5 transition", monOpen && "rotate-180")}
				aria-hidden="true"
			/>
		</button>
		<DropdownMenu
			open={monOpen}
			anchor={monBtn}
			onClose={() => (facet = null)}
			minWidth="12rem"
			ariaLabel={i18n.monitor_monitoring()}
		>
			{#each monOptions as o (o.key)}
				<DropdownOption
					label={o.label}
					icon={o.icon}
					count={monCount(o.key)}
					selected={mon === o.key}
					onSelect={() => pickMon(o.key)}
				/>
			{/each}
		</DropdownMenu>
	</div>

	<!-- No forced break: the row wraps only where it runs out of width. A break
	     pinned to a breakpoint fired at widths that fit one line and read as an
	     arbitrary split. When it does wrap, the facets and the field fill the
	     first line and the action group takes the second, right-aligned by
	     `ml-auto` — 768px of tablet width is 698px of content, which two keyed
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
				onclick={() => (sortOpen = !sortOpen)}
				aria-haspopup="listbox"
				aria-expanded={sortOpen}
				class="inline-flex h-11 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus:ring-2 focus:ring-accent-ring lg:h-9"
			>
				<!-- The key word is the first thing to go when the line is tight; the
				     value alone still reads as a sort. -->
				<span class="sort-key hidden text-fg-subtle">{i18n.filter_sort()}</span>
				<!-- Fixed width: the labels differ in length, and letting the trigger
				     resize moved every control to its right on each pick. -->
				<span class="text-left text-fg lg:w-[5.5rem]">{currentSortLabel}</span>
				<ChevronDown
					class={cn("h-3.5 w-3.5 transition", sortOpen && "rotate-180")}
					aria-hidden="true"
				/>
			</button>
			<DropdownMenu
				open={sortOpen}
				anchor={sortBtn}
				onClose={() => (sortOpen = false)}
				align="end"
				minWidth="12rem"
				ariaLabel={i18n.filter_sort()}
			>
				{#each sortOptions as opt (opt.key)}
					<DropdownOption
						label={opt.label}
						selected={currentSortKey === opt.key}
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

		<!-- Selection is a mode, not a filter, so it gets one icon rather than the
		     old labelled pair — and only in grid view, where the cards' own
		     checkboxes are hidden until hover. Select all joins it whenever the mode is
		     on, at every width from md up: the bulk bar carries one too, but that bar only exists after
		     something has been picked, which left the empty selection with no way to
		     take everything. It follows the active filters, so it takes what is
		     loaded under them rather than the library. -->
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
					selectMode
						? "border-accent-line bg-accent-soft text-accent-text"
						: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
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
			onclick={onAddMovie}
			class="hidden h-11 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:inline-flex lg:h-9"
		>
			<Plus size={14} aria-hidden="true" />
			<span class="add-long hidden">{i18n.action_add_movie()}</span>
			<span class="add-short">{i18n.common_add()}</span>
		</button>
	</div>
</div>

<style>
	/* ── Fit, measured against the row and not the window ─────────────────────
	   The breakpoints are the viewport's, and this row is not: the sidebar takes
	   248px of it from the width the sidebar expands at, so `lg` fires with about
	   712px of row to play with. Pinning the field at `lg:w-60` there left the
	   first line 240px of field and a 330px hole while the action group wrapped
	   below it — the break read as arbitrary because the line it broke was not
	   full. The row is its own container instead, so the three things that decide
	   whether one line fits answer to the space they are actually in. */
	.lib-toolbar {
		container-type: inline-size;
		container-name: libtoolbar;
	}
	/* The field takes the slack at every width — the `flex-1` it is marked with,
	   no container rule of its own. It was pinned to 15rem from 990px up, which
	   left a 185px hole between it and the action group, and pinned it wrapped:
	   flex breaks lines on an item's basis, before any shrinking, so select
	   mode's extra "Select all N" pushed the whole group onto a second line and
	   the grid below it jumped down. A zero basis can do neither. */
	@container libtoolbar (min-width: 990px) {
		.sort-key {
			display: inline;
		}
	}
	/* The key words go before the row does. "Status" and "Monitoring" are worth
	   116px between them, and the dot, the eye and the value carry the meaning
	   without them. */
	@container libtoolbar (max-width: 989.98px) {
		.facet-key {
			display: none;
		}
	}
	/* The verb spells itself out once the row can pay the 40px for it — also a
	   container call, not a breakpoint: at 1100px of window with the sidebar out,
	   "Add movie" was what pushed the row over. */
	@container libtoolbar (min-width: 900px) {
		.add-long {
			display: inline;
		}
		.add-short {
			display: none;
		}
	}
</style>
