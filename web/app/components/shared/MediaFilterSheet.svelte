<script lang="ts" module>
	export type FilterOption = { key: string; label: string };
</script>

<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import type { Snippet } from "svelte";
	import {
		X,
		Search,
		LayoutGrid,
		List,
		ListChecks,
	} from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { dragScroll } from "@lib/drag-scroll";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { NOUN_TITLE, type Noun } from "@lib/nouns";

	// Everything the library toolbars can't afford to keep on screen at
	// phone width: the filter query, sort, view and the way into
	// selection. One sheet, one trip.
	let {
		open,
		onClose,
		noun = NOUN_TITLE,
		query,
		onQueryChange,
		sortOptions,
		sort,
		onSortChange,
		view,
		onViewChange,
		onSelectMode,
		onReset,
		activeCount = 0,
		extra,
	}: {
		open: boolean;
		onClose: () => void;
		noun?: Noun;
		query: string;
		onQueryChange: (q: string) => void;
		sortOptions: FilterOption[];
		sort: string;
		onSortChange: (key: string) => void;
		view: "grid" | "list";
		onViewChange: (v: "grid" | "list") => void;
		onSelectMode?: () => void;
		onReset: () => void;
		activeCount?: number;
		// Filters only one media type has — the series type pills.
		extra?: Snippet;
	} = $props();


	const chip =
		"inline-flex h-11 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff =
		"border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 eyebrow text-[9.5px] text-fg-subtle";
</script>

{#if open}
	<BottomSheet
		title={i18n.filter_label()}
		label={i18n.filter_and_sort()}
		{onClose}
	>
		<div
			class="flex h-11 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
		>
			<Search class="h-4 w-4 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={query}
				oninput={(e) => onQueryChange(e.currentTarget.value)}
				placeholder={i18n.common_filter_items({ items: noun.items })}
				class="min-w-0 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-fg-subtle"
			/>
			{#if query}
				<button
					type="button"
					onclick={() => onQueryChange("")}
					aria-label={i18n.common_clear_search()}
					class="grid h-7 w-7 place-items-center rounded-full text-fg-faint transition active:bg-surface"
				>
					<X size={14} aria-hidden="true" />
				</button>
			{/if}
		</div>

		<div class="pt-5">
			<div class={label}>{i18n.filter_sort()}</div>
			<!-- One scrolling line rather than a wrap, like the status pills on the
			     toolbar: the group keeps its height however many sorts a page has.
			     The right edge fades, so a cut chip reads as more to scroll to. -->
			<div
				use:dragScroll
				class="sort-scroll -mx-5 flex items-center gap-2 overflow-x-auto px-5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
			>
				{#each sortOptions as opt (opt.key)}
					{@const on = sort === opt.key}
					<button
						type="button"
						aria-pressed={on}
						onclick={() => onSortChange(opt.key)}
						class={twMerge(chip, on ? chipOn : chipOff)}
					>
						{opt.label}
					</button>
				{/each}
			</div>
		</div>

		{#if extra}
			{@render extra()}
		{/if}

		<!-- The list table needs columns a phone cannot give it, so the choice
		     only appears from md up; below that the library is posters only. -->
		<div class="hidden pt-5 md:block">
			<div class={label}>{i18n.filter_layout()}</div>
			<div class="flex flex-wrap gap-2">
				<button
					type="button"
					aria-pressed={view === "grid"}
					onclick={() => onViewChange("grid")}
					class={twMerge(chip, view === "grid" ? chipOn : chipOff)}
				>
					<LayoutGrid size={14} aria-hidden="true" />
					{i18n.filter_posters()}
				</button>
				<button
					type="button"
					aria-pressed={view === "list"}
					onclick={() => onViewChange("list")}
					class={twMerge(chip, view === "list" ? chipOn : chipOff)}
				>
					<List size={14} aria-hidden="true" />
					{i18n.filter_list()}
				</button>
			</div>
		</div>
		{#snippet footer()}
			<!-- List rows have their own checkboxes; only the poster grid needs a way
			     into selection. -->
			{#if view === "grid" && onSelectMode}
				<button
					type="button"
					onclick={() => {
						onSelectMode();
						onClose();
					}}
					class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2"
				>
					<ListChecks size={16} aria-hidden="true" />
					{i18n.a11y_select_all_of({ items: noun.items })}
				</button>
			{/if}
			<button
				type="button"
				disabled={activeCount === 0}
				onclick={onReset}
				class={twMerge(
					"inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40",
					view === "list" && "flex-1 border border-border bg-surface",
				)}
			>
				{i18n.common_reset()}
			</button>
		{/snippet}
	</BottomSheet>
{/if}

<style>
	/* The row bleeds to the sheet's edges, so a chip that runs past the right
	   would otherwise look clipped rather than scrollable. */
	.sort-scroll {
		-webkit-mask-image: linear-gradient(
			to right,
			transparent 0,
			#000 20px,
			#000 calc(100% - 28px),
			transparent 100%
		);
		mask-image: linear-gradient(
			to right,
			transparent 0,
			#000 20px,
			#000 calc(100% - 28px),
			transparent 100%
		);
	}
</style>
