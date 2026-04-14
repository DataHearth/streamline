<script lang="ts">
	import { Search, Rows3, Rows2, X, Trash2 } from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	type View = "queue" | "history";
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
	} = $props();

	const CHIPS: Record<View, { key: string; label: string }[]> = {
		queue: [
			{ key: "downloading", label: "Downloading" },
			{ key: "importing", label: "Importing" },
			{ key: "paused", label: "Paused" },
			{ key: "error", label: "Error" },
		],
		history: [
			{ key: "completed", label: "Completed" },
			{ key: "failed", label: "Failed" },
		],
	};

	let chips = $derived(CHIPS[view]);

	function toggleChip(key: string) {
		onStatusFilterChange(
			statusFilter.includes(key)
				? statusFilter.filter((s) => s !== key)
				: [...statusFilter, key],
		);
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
		{#each [{ k: "queue", l: "Queue" }, { k: "history", l: "History" }] as t (t.k)}
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

	<div class="flex flex-wrap items-center gap-1.5">
		{#each chips as c (c.key)}
			<button
				type="button"
				aria-pressed={statusFilter.includes(c.key)}
				onclick={() => toggleChip(c.key)}
				class={cn(
					"inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-medium transition",
					statusFilter.includes(c.key)
						? "border-accent bg-accent/15 text-accent"
						: "border-border text-fg-muted hover:border-fg-faint hover:text-fg",
				)}
			>
				{c.label}
				{#if statusFilter.includes(c.key)}
					<X size={12} aria-hidden="true" />
				{/if}
			</button>
		{/each}
	</div>

	<div class="ml-auto flex items-center gap-2">
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
				placeholder="Filter title or movie…"
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
