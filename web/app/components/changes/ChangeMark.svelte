<script lang="ts">
	import { CircleX, TriangleAlert } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import type { CoverMark } from "@lib/metadata-changes";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The badge on a cover whose item has a provider change waiting: amber for
	// a change, red for an item the provider removed. Compact is the icon
	// alone, for a thumbnail too small to carry the word.
	let {
		mark,
		compact = false,
		class: klass,
	}: { mark: CoverMark | undefined; compact?: boolean; class?: string } = $props();

	const label = $derived(mark === "removed" ? i18n.change_mark_removed() : i18n.change_mark_changed());
	const tone = $derived(mark === "removed" ? "bg-status-failed text-white" : "bg-status-wanted text-bg-deep");
	const base =
		"pointer-events-none absolute z-10 inline-flex items-center gap-1 text-[10.5px] font-semibold leading-none shadow-[var(--shadow-1)]";
</script>

{#snippet icon()}
	{#if mark === "removed"}
		<CircleX size={11} aria-hidden="true" />
	{:else}
		<TriangleAlert size={11} aria-hidden="true" />
	{/if}
{/snippet}

{#if mark && compact}
	<span role="img" aria-label={label} title={label} class={twMerge(base, "right-0.5 top-0.5 rounded-full p-0.5", tone, klass)}>
		{@render icon()}
	</span>
{:else if mark}
	<span class={twMerge(base, "right-2 top-2 rounded px-1.5 py-1", tone, klass)}>
		{@render icon()}
		{label}
	</span>
{/if}
