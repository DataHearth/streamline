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
	} from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";

	type View = "grid" | "list";

	let {
		tab,
		typeFilter,
		query,
		sort,
		view,
		counts,
		onTabChange,
		onTypeChange,
		onQueryChange,
		onSortChange,
		onViewChange,
		onAddSeries,
	}: {
		tab: SeriesTab;
		typeFilter: SeriesTypeFilter;
		query: string;
		sort: SeriesSort;
		view: View;
		counts: SeriesTabCounts;
		onTabChange: (t: SeriesTab) => void;
		onTypeChange: (t: SeriesTypeFilter) => void;
		onQueryChange: (q: string) => void;
		onSortChange: (s: SeriesSort) => void;
		onViewChange: (v: View) => void;
		onAddSeries: () => void;
	} = $props();

	const tabs: { key: SeriesTab; label: string }[] = [
		{ key: "all", label: "All" },
		{ key: "continuing", label: "Continuing" },
		{ key: "ended", label: "Ended" },
		{ key: "upcoming", label: "Upcoming" },
		{ key: "missing", label: "Missing eps" },
	];

	const typePills: { key: SeriesTypeFilter; label: string }[] = [
		{ key: "all", label: "all" },
		{ key: "standard", label: "standard" },
		{ key: "anime", label: "anime" },
		{ key: "daily", label: "daily" },
	];

	const sortOptions: { key: SeriesSort; label: string }[] = [
		{ key: "recent", label: "Recently added" },
		{ key: "title", label: "Title A→Z" },
		{ key: "year", label: "Year newest" },
		{ key: "rating", label: "Rating highest" },
		{ key: "episodes", label: "Most episodes" },
	];

	let sortOpen = $state(false);
	let sortRoot = $state<HTMLDivElement | null>(null);

	let currentSortLabel = $derived(
		sortOptions.find((o) => o.key === sort)?.label ?? "Recently added",
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
</script>

<div
	class="sticky top-16 z-20 flex flex-col gap-3 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex-row md:flex-wrap md:items-center md:justify-between md:gap-4 md:px-6"
>
	<nav
		aria-label="Series status"
		class="filter-tabs flex w-full items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-bg-elevated p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden md:w-auto md:shrink-0"
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
						"rounded-sm px-1.5 py-px font-mono text-[10px] tabular",
						active
							? "bg-accent-soft text-accent-text"
							: "bg-white/[0.04] text-fg-faint",
					)}
				>
					{counts[t.key]}
				</span>
			</button>
		{/each}
	</nav>

	<div class="flex flex-col gap-2 md:flex-1 md:flex-row md:flex-wrap md:items-center md:justify-end">
		<div
			class="inline-flex max-w-full items-center gap-0.5 overflow-x-auto rounded-md border border-border bg-bg-elevated p-[3px] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden md:max-w-none"
			role="group"
			aria-label="Series type"
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

		<div
			class="search-wrap flex h-9 w-full items-center gap-2 rounded-md border border-border bg-bg-elevated px-3 transition focus-within:border-accent md:w-auto"
		>
			<Search class="h-3.5 w-3.5 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={query}
				oninput={(e) => onQueryChange(e.currentTarget.value)}
				placeholder="Filter…"
				class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint md:w-44 md:flex-none"
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

		<div class="flex items-center gap-2 md:contents">
		<div bind:this={sortRoot} class="relative">
			<button
				type="button"
				onclick={() => (sortOpen = !sortOpen)}
				aria-haspopup="listbox"
				aria-expanded={sortOpen}
				class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus:ring-2 focus:ring-accent-ring"
			>
				<span class="text-fg-subtle">Sort</span>
				<span class="text-fg">{currentSortLabel}</span>
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
			aria-label="View mode"
		>
			<button
				type="button"
				onclick={() => onViewChange("grid")}
				title="Grid view"
				class={cn(
					"grid h-[30px] w-[30px] place-items-center rounded-sm transition",
					view === "grid" ? "bg-bg-card text-fg" : "text-fg-subtle hover:text-fg",
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
					view === "list" ? "bg-bg-card text-fg" : "text-fg-subtle hover:text-fg",
				)}
			>
				<List size={14} aria-hidden="true" />
				<span class="sr-only">List view</span>
			</button>
		</div>

		<button
			type="button"
			onclick={onAddSeries}
			class="inline-flex h-9 flex-1 items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:flex-none"
		>
			<Plus size={14} aria-hidden="true" />
			Add series
		</button>
		</div>
	</div>
</div>
