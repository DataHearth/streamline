<script lang="ts" module>
	export type FilterOption = { key: string; label: string };
	export type Density = "compact" | "roomy";
</script>

<script lang="ts">
	import type { Snippet } from "svelte";
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import {
		X,
		Search,
		Bookmark,
		LayoutGrid,
		List,
		Rows2,
		Rows3,
		ListChecks,
		Check,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";

	// Everything the library toolbars can no longer afford to keep on screen at
	// phone width: the filter query, sort, monitored, view, density and the way
	// into selection. One sheet, one trip.
	let {
		open,
		onClose,
		noun = "titles",
		query,
		onQueryChange,
		sortOptions,
		sort,
		onSortChange,
		monitoredOnly,
		monitoredCount,
		onMonitoredChange,
		view,
		onViewChange,
		density,
		onDensityChange,
		onSelectMode,
		onReset,
		activeCount = 0,
		extra,
	}: {
		open: boolean;
		onClose: () => void;
		noun?: string;
		query: string;
		onQueryChange: (q: string) => void;
		sortOptions: FilterOption[];
		sort: string;
		onSortChange: (key: string) => void;
		monitoredOnly: boolean;
		monitoredCount: number;
		onMonitoredChange: (v: boolean) => void;
		view: "grid" | "list";
		onViewChange: (v: "grid" | "list") => void;
		density: Density;
		onDensityChange: (v: Density) => void;
		onSelectMode: () => void;
		onReset: () => void;
		activeCount?: number;
		// Filters only one media type has — the series type pills.
		extra?: Snippet;
	} = $props();

	$effect(() => {
		if (!open) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

	const chip =
		"inline-flex h-9 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff =
		"border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint";
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label="Filter and sort"
	>
		<button
			type="button"
			aria-label="Close"
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">Filter</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label="Close"
					class="grid h-9 w-9 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2"
			>
				<div
					class="flex h-11 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
				>
					<Search class="h-4 w-4 text-fg-subtle" aria-hidden="true" />
					<input
						type="search"
						value={query}
						oninput={(e) => onQueryChange(e.currentTarget.value)}
						placeholder="Filter {noun}…"
						class="min-w-0 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-fg-faint"
					/>
					{#if query}
						<button
							type="button"
							onclick={() => onQueryChange("")}
							aria-label="Clear search"
							class="grid h-7 w-7 place-items-center rounded-full text-fg-faint transition active:bg-surface"
						>
							<X size={14} aria-hidden="true" />
						</button>
					{/if}
				</div>

				<div class="pt-5">
					<div class={label}>Sort</div>
					<div class="flex flex-wrap gap-2">
						{#each sortOptions as opt (opt.key)}
							{@const on = sort === opt.key}
							<button
								type="button"
								aria-pressed={on}
								onclick={() => onSortChange(opt.key)}
								class={cn(chip, on ? chipOn : chipOff)}
							>
								{opt.label}
								<!-- The tick is always laid out, only hidden: revealing it on pick
								     changed the chip's width and reflowed the whole wrap. -->
								<Check
									size={14}
									class={on ? "" : "invisible"}
									aria-hidden="true"
								/>
							</button>
						{/each}
					</div>
				</div>

				{#if extra}
					{@render extra()}
				{/if}

				<div class="pt-5">
					<div class={label}>Show</div>
					<button
						type="button"
						aria-pressed={monitoredOnly}
						onclick={() => onMonitoredChange(!monitoredOnly)}
						class={cn(chip, monitoredOnly ? chipOn : chipOff)}
					>
						<Bookmark
							size={14}
							fill={monitoredOnly ? "currentColor" : "none"}
							aria-hidden="true"
						/>
						Monitored only
						<span class="font-mono text-[11px] tabular-nums opacity-70">
							{monitoredCount}
						</span>
					</button>
				</div>

				<div class="pt-5">
					<div class={label}>Layout</div>
					<!-- The list table needs columns a phone cannot give it, so the choice
					     only appears from md up; below that the library is posters only. -->
					<div class="hidden flex-wrap gap-2 md:flex">
						<button
							type="button"
							aria-pressed={view === "grid"}
							onclick={() => onViewChange("grid")}
							class={cn(chip, view === "grid" ? chipOn : chipOff)}
						>
							<LayoutGrid size={14} aria-hidden="true" />
							Posters
						</button>
						<button
							type="button"
							aria-pressed={view === "list"}
							onclick={() => onViewChange("list")}
							class={cn(chip, view === "list" ? chipOn : chipOff)}
						>
							<List size={14} aria-hidden="true" />
							List
						</button>
					</div>
					{#if view === "grid"}
						<div class="flex flex-wrap gap-2 md:mt-2">
							<button
								type="button"
								aria-pressed={density === "compact"}
								onclick={() => onDensityChange("compact")}
								class={cn(chip, density === "compact" ? chipOn : chipOff)}
							>
								<Rows3 size={14} aria-hidden="true" />
								Compact
							</button>
							<button
								type="button"
								aria-pressed={density === "roomy"}
								onclick={() => onDensityChange("roomy")}
								class={cn(chip, density === "roomy" ? chipOn : chipOff)}
							>
								<Rows2 size={14} aria-hidden="true" />
								Roomy
							</button>
						</div>
					{/if}
				</div>
			</div>

			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
				<!-- List rows have their own checkboxes; only the poster grid needs a way
				     into selection. -->
				{#if view === "grid"}
					<button
						type="button"
						onclick={() => {
							onSelectMode();
							onClose();
						}}
						class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2"
					>
						<ListChecks size={16} aria-hidden="true" />
						Select {noun}
					</button>
				{/if}
				<button
					type="button"
					disabled={activeCount === 0}
					onclick={onReset}
					class={cn(
						"inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40",
						view === "list" && "flex-1 border border-border bg-surface",
					)}
				>
					Reset
				</button>
			</div>
		</div>
	</div>
{/if}
