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
		onTabChange,
		onQueryChange,
		onSortChange,
		onViewChange,
		onAddMovie,
	}: {
		tab: string;
		query: string;
		sort: SortKey;
		order: SortOrder;
		view: View;
		// Only the per-status tallies are shown here; `trend` (from /movies/counts)
		// isn't needed, so accept the client-computed counts without it.
		counts: Omit<MovieCounts, "trend">;
		onTabChange: (t: string) => void;
		onQueryChange: (q: string) => void;
		onSortChange: (s: SortKey, o: SortOrder) => void;
		onViewChange: (v: View) => void;
		onAddMovie: () => void;
	} = $props();

	const tabs = [
		{ key: "all", label: "All" },
		{ key: "available", label: "Available" },
		{ key: "downloading", label: "Downloading" },
		{ key: "wanted", label: "Wanted" },
		{ key: "failed", label: "Failed" },
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
				return 0;
			default:
				return 0;
		}
	}
</script>

<div
	class="sticky top-16 z-20 flex flex-col gap-3 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex-row md:flex-wrap md:items-center md:justify-between md:gap-4 md:px-6"
>
	<nav
		aria-label="Movie status"
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
				<span>{t.label}</span>
				<span
					class={cn(
						"rounded-sm px-1.5 py-px font-mono text-[10px] tabular",
						active
							? "bg-accent-soft text-accent-text"
							: "bg-white/[0.04] text-fg-faint",
					)}
				>
					{tabCount(t.key)}
				</span>
			</button>
		{/each}
	</nav>

	<div class="flex flex-col gap-2 md:flex-1 md:flex-row md:flex-wrap md:items-center md:justify-end">
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
			class="inline-flex h-9 flex-1 items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow md:flex-none"
		>
			<Plus size={14} aria-hidden="true" />
			Add movie
		</button>
		</div>
	</div>
</div>
