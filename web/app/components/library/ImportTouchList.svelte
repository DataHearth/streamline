<script lang="ts">
	import { Search, X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import {
		CLASS_CHIPS,
		type TouchEntry,
	} from "../../lib/imports-touch";
	import type { ImportFileClassification } from "../../lib/types";
	import ImportTouchRow from "./ImportTouchRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// E2: search stays in the open and classification is a chip scroller rather
	// than a sheet — five options with a colour each are already chips elsewhere
	// here, and the dots on the rows speak the same vocabulary.
	let {
		entries,
		total,
		series = false,
		query,
		onQueryChange,
		classification,
		onClassificationChange,
		pending = false,
		error,
		onOpen,
	}: {
		entries: TouchEntry[];
		total: number;
		series?: boolean;
		query: string;
		onQueryChange: (q: string) => void;
		classification: "" | ImportFileClassification;
		onClassificationChange: (c: "" | ImportFileClassification) => void;
		pending?: boolean;
		error?: string;
		onOpen: (entry: TouchEntry) => void;
	} = $props();

	let noun = $derived(series ? "shows" : "files");
	const DOT: Record<string, string> = {
		confirmed: "available",
		ambiguous: "wanted",
		unmatched: "paused",
		existing: "grabbing",
	};
</script>

<section class="mt-5 overflow-hidden rounded-lg border border-border bg-bg-elevated lg:hidden">
	<header class="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
		<h2 class="text-base font-semibold text-fg">{series ? i18n.common_shows() : i18n.common_files()}</h2>
		{#if total > 0}
			<span class="font-mono text-xs tabular-nums text-fg-subtle">{total}</span>
		{/if}
	</header>

	<div class="border-b border-border px-3.5 pb-3 pt-3">
		<div
			class="flex h-10 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3 transition focus-within:border-accent"
		>
			<Search class="h-4 w-4 shrink-0 text-fg-subtle" aria-hidden="true" />
			<input
				type="search"
				value={query}
				oninput={(e) => onQueryChange(e.currentTarget.value)}
				placeholder={series ? i18n.imports_search_folder_title() : i18n.imports_search_filename()}
				class="min-w-0 flex-1 bg-transparent text-[14px] text-fg outline-none placeholder:text-fg-faint"
			/>
			{#if query}
				<button
					type="button"
					onclick={() => onQueryChange("")}
					aria-label={i18n.common_clear_search()}
					class="grid h-7 w-7 shrink-0 place-items-center rounded-full text-fg-faint transition active:bg-surface"
				>
					<X size={14} aria-hidden="true" />
				</button>
			{/if}
		</div>

		<div
			use:dragScroll
			class="chip-scroll -mx-3.5 mt-2.5 flex items-center gap-2 overflow-x-auto px-3.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
		>
			{#each CLASS_CHIPS as chip (chip.key)}
				{@const on = classification === chip.key}
				<button
					type="button"
					aria-pressed={on}
					onclick={() => onClassificationChange(chip.key)}
					class={cn(
						"inline-flex h-11 shrink-0 items-center gap-1.5 rounded-full border px-3 text-[12.5px] font-medium transition",
						on
							? "border-accent-line bg-accent-soft text-accent-text"
							: "border-border bg-surface text-fg-muted active:bg-surface-2",
					)}
				>
					{#if chip.key}
						<span
							class="h-1.5 w-1.5 shrink-0 rounded-full"
							style:background-color="var(--status-{DOT[chip.key]})"
							aria-hidden="true"
						></span>
					{/if}
					{chip.label}
				</button>
			{/each}
		</div>
	</div>

	{#if pending}
		<p class="px-4 py-8 text-sm text-fg-subtle">Loading {noun}…</p>
	{:else if error}
		<p class="px-4 py-8 text-sm text-status-failed">Failed: {error}</p>
	{:else if entries.length === 0}
		<p class="px-4 py-8 text-sm text-fg-muted">No {noun} match this filter.</p>
	{:else}
		<ul class="divide-y divide-border">
			{#each entries as e (e.id)}
				<li>
					<ImportTouchRow entry={e} {series} wide {onOpen} />
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.chip-scroll {
		-webkit-mask-image: linear-gradient(
			to right,
			transparent 0,
			#000 14px,
			#000 calc(100% - 22px),
			transparent 100%
		);
		mask-image: linear-gradient(
			to right,
			transparent 0,
			#000 14px,
			#000 calc(100% - 22px),
			transparent 100%
		);
	}
</style>
