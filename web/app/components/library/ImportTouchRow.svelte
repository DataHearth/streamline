<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { outcomeWord, type TouchEntry } from "../../lib/imports-touch";

	let {
		entry,
		series = false,
		wide = false,
		onOpen,
	}: {
		entry: TouchEntry;
		series?: boolean;
		// From md up the row keeps its shape and gains two trailing columns
		// instead of the chevron — same component at 390 and at 834.
		wide?: boolean;
		onOpen: (entry: TouchEntry) => void;
	} = $props();

	let word = $derived(outcomeWord(entry, series));
	const TONE: Record<string, string> = {
		need: "text-status-wanted",
		ok: "text-status-available",
		link: "text-status-grabbing",
		muted: "text-fg-subtle",
		fail: "text-status-failed",
	};
</script>

<button
	type="button"
	onclick={() => onOpen(entry)}
	class="flex w-full items-center gap-3 px-3.5 py-2.5 text-left transition active:bg-bg-card md:px-4"
>
	<span
		class="h-2 w-2 shrink-0 rounded-full"
		style:background-color="var(--status-{entry.classification === 'confirmed'
			? 'available'
			: entry.classification === 'ambiguous'
				? 'wanted'
				: entry.classification === 'existing'
					? 'grabbing'
					: 'paused'})"
		aria-hidden="true"
	></span>

	<span class="min-w-0 flex-1">
		<span
			class={cn(
				"block truncate text-[13.5px] font-semibold tracking-[-0.01em]",
				entry.headingWeak ? "font-mono text-[12.5px] text-fg-muted" : "text-fg",
			)}
		>
			{entry.heading}
		</span>
		<span class="mt-0.5 block truncate font-mono text-[11px] text-fg-subtle">
			{entry.sub}
		</span>
	</span>

	{#if wide}
		<span
			class="hidden shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold md:inline-flex"
			style:color="var(--status-{entry.classification === 'confirmed'
				? 'available'
				: entry.classification === 'ambiguous'
					? 'wanted'
					: entry.classification === 'existing'
						? 'grabbing'
						: 'paused'})"
			style:background-color="color-mix(in srgb, var(--status-{entry.classification ===
			'confirmed'
				? 'available'
				: entry.classification === 'ambiguous'
					? 'wanted'
					: entry.classification === 'existing'
						? 'grabbing'
						: 'paused'}) 14%, transparent)"
		>
			{entry.classification.charAt(0).toUpperCase() +
				entry.classification.slice(1)}
		</span>
	{/if}

	<span
		class={cn(
			"shrink-0 text-[12px] font-medium whitespace-nowrap",
			TONE[word.tone],
		)}
	>
		{word.text}
	</span>
	<ChevronRight size={14} class="shrink-0 text-fg-faint" aria-hidden="true" />
</button>
