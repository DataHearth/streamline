<script lang="ts">
	import MovieDetailCast from "../movies/MovieDetailCast.svelte";
	import type { CastMember } from "../../lib/types";

	let {
		overview,
		cast = [],
		onViewAllCast,
	}: {
		overview?: string;
		cast?: CastMember[];
		onViewAllCast?: () => void;
	} = $props();

	let topCast = $derived(cast.slice(0, 5));
</script>

<section class="min-w-0" aria-labelledby="detail-synopsis">
	<h2
		id="detail-synopsis"
		class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
	>
		Synopsis
	</h2>
	{#if overview}
		<p
			class="mt-3 max-w-[720px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
		>
			{overview}
		</p>
	{:else}
		<p class="mt-3 text-sm italic text-fg-subtle">No overview available.</p>
	{/if}

	{#if topCast.length > 0}
		<div class="my-5 h-px bg-border"></div>
		<div class="mb-3 flex items-baseline justify-between">
			<h3
				class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
			>
				Cast
			</h3>
			{#if cast.length > topCast.length && onViewAllCast}
				<button
					type="button"
					onclick={onViewAllCast}
					class="font-mono text-[11px] text-accent-text transition hover:text-accent"
				>
					View all
				</button>
			{/if}
		</div>
		<MovieDetailCast cast={topCast} dense />
	{/if}
</section>
