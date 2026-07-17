<script lang="ts">
	import { Search, Rows3, Rows2, X, Trash2, Plus, ListFilter, Check } from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";

	type View = "queue" | "history" | "torrents";
	type Density = "comfortable" | "compact";

	let {
		view,
		statusFilter,
		search,
		density,
		onViewChange,
		onStatusFilterChange,
		onSearchChange,
		onDensityToggle,
		onClearCompleted,
		clearableCount = 0,
		onAddTorrent,
		canAddTorrent = false,
	}: {
		view: View;
		statusFilter: string[];
		search: string;
		density: Density;
		onViewChange: (v: View) => void;
		onStatusFilterChange: (s: string[]) => void;
		onSearchChange: (q: string) => void;
		onDensityToggle: () => void;
		onClearCompleted?: () => void;
		clearableCount?: number;
		onAddTorrent?: () => void;
		canAddTorrent?: boolean;
	} = $props();

	// `dot` maps each filter to a real --status-* token (some chip keys like
	// "importing"/"error" have no token of their own — mirror lib/format.pillStatus).
	const CHIPS: Record<View, { key: string; label: string; dot: string }[]> = {
		queue: [
			{ key: "downloading", label: "Downloading", dot: "downloading" },
			{ key: "importing", label: "Importing", dot: "grabbing" },
			{ key: "paused", label: "Paused", dot: "paused" },
			{ key: "error", label: "Error", dot: "failed" },
		],
		history: [
			{ key: "completed", label: "Completed", dot: "available" },
			{ key: "failed", label: "Failed", dot: "failed" },
		],
		torrents: [
			{ key: "downloading", label: "Downloading", dot: "downloading" },
			{ key: "stalled", label: "Stalled", dot: "stalled" },
			{ key: "seeding", label: "Seeding", dot: "seeding" },
			{ key: "completed", label: "Completed", dot: "completed" },
			{ key: "paused", label: "Paused", dot: "paused" },
		],
	};

	let chips = $derived(CHIPS[view]);
	let filterOpen = $state(false);
	let activeChips = $derived(chips.filter((c) => statusFilter.includes(c.key)));

	function toggleChip(key: string) {
		onStatusFilterChange(
			statusFilter.includes(key)
				? statusFilter.filter((s) => s !== key)
				: [...statusFilter, key],
		);
	}

	// Close the popover on outside click / Escape.
	function filterPopover(node: HTMLElement) {
		const onDoc = (e: MouseEvent) => {
			if (!node.contains(e.target as Node)) filterOpen = false;
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") filterOpen = false;
		};
		document.addEventListener("mousedown", onDoc);
		document.addEventListener("keydown", onKey);
		return {
			destroy() {
				document.removeEventListener("mousedown", onDoc);
				document.removeEventListener("keydown", onKey);
			},
		};
	}
</script>

<div
	class="sticky top-16 z-20 -mx-4 mb-4 flex flex-wrap items-center gap-3 bg-bg-deep/85 px-4 pb-2 pt-3 backdrop-blur md:-mx-6 md:px-6 lg:-mx-8 lg:px-8"
>
	<div
		class="relative inline-flex items-center gap-1 border-b border-border"
		role="tablist"
		aria-label="Activity view"
	>
		{#each [{ k: "queue", l: "Queue" }, { k: "history", l: "History" }, { k: "torrents", l: "Torrents" }] as t (t.k)}
			{@const active = view === t.k}
			<button
				type="button"
				role="tab"
				aria-selected={active}
				onclick={() => onViewChange(t.k as View)}
				class={cn(
					"relative flex items-center gap-1.5 px-4 py-3 text-[13px] font-medium transition",
					active
						? "text-fg after:absolute after:inset-x-3 after:-bottom-px after:h-0.5 after:bg-accent"
						: "text-fg-subtle hover:text-fg",
				)}
			>
				{t.l}
			</button>
		{/each}
	</div>

	<div class="relative" use:filterPopover>
		<button
			type="button"
			onclick={() => (filterOpen = !filterOpen)}
			aria-haspopup="true"
			aria-expanded={filterOpen}
			class={cn(
				"inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition",
				activeChips.length > 0
					? "border-accent/60 bg-accent/10 text-accent"
					: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
			)}
		>
			<ListFilter size={15} aria-hidden="true" />
			<span>Status</span>
			{#if activeChips.length > 0}
				<span
					class="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-fg-on-accent"
				>
					{activeChips.length}
				</span>
			{/if}
		</button>

		{#if filterOpen}
			<div
				transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
				class="absolute left-0 top-full z-30 mt-1.5 w-52 origin-top overflow-hidden rounded-md border border-border-strong bg-bg-elevated p-1 shadow-4"
				role="menu"
			>
				{#each chips as c (c.key)}
					{@const on = statusFilter.includes(c.key)}
					<button
						type="button"
						role="menuitemcheckbox"
						aria-checked={on}
						onclick={() => toggleChip(c.key)}
						class="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						<span
							class={cn(
								"grid h-4 w-4 shrink-0 place-items-center rounded border transition",
							on
								? "border-accent bg-accent text-fg-on-accent"
								: "border-border-strong",
							)}
						>
							{#if on}<Check size={11} aria-hidden="true" />{/if}
						</span>
						<span class="flex items-center gap-1.5">
							<span
								class="h-1.5 w-1.5 shrink-0 rounded-full"
								style:background-color="var(--status-{c.dot})"
							></span>
							{c.label}
						</span>
					</button>
				{/each}
				{#if activeChips.length > 0}
					<div class="my-1 border-t border-border"></div>
					<button
						type="button"
						onclick={() => onStatusFilterChange([])}
						class="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs font-medium text-fg-subtle transition hover:bg-surface hover:text-fg"
					>
						<X size={12} aria-hidden="true" />
						Clear filters
					</button>
				{/if}
			</div>
		{/if}
	</div>

	<div class="ml-auto flex items-center gap-2">
		{#if view === "torrents" && onAddTorrent && canAddTorrent}
			<button
				type="button"
				onclick={() => onAddTorrent?.()}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
			>
				<Plus size={15} aria-hidden="true" />
				Add torrent
			</button>
		{/if}
		{#if view === "history" && onClearCompleted}
			<button
				type="button"
				onclick={() => onClearCompleted?.()}
				disabled={clearableCount === 0}
				class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
			>
				<Trash2 size={14} aria-hidden="true" />
				Clear completed{clearableCount > 0 ? ` (${clearableCount})` : ""}
			</button>
		{/if}
		<div class="relative">
			<Search
				size={15}
				class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-faint"
				aria-hidden="true"
			/>
			<input
				type="search"
				value={search}
				oninput={(e) => onSearchChange(e.currentTarget.value)}
				placeholder={view === "torrents"
					? "Filter name or hash…"
					: "Filter title or movie…"}
				aria-label="Filter activity"
				class="h-9 w-48 rounded-md border border-border bg-bg-elevated pl-8 pr-3 text-sm text-fg placeholder:text-fg-faint focus:border-accent focus:outline-none"
			/>
		</div>
		<button
			type="button"
			onclick={onDensityToggle}
			aria-label={density === "comfortable"
				? "Switch to compact rows"
				: "Switch to comfortable rows"}
			title="Row density"
			class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-border bg-bg-elevated text-fg-muted transition hover:text-fg"
		>
			{#if density === "comfortable"}
				<Rows2 size={16} aria-hidden="true" />
			{:else}
				<Rows3 size={16} aria-hidden="true" />
			{/if}
		</button>
	</div>
</div>
