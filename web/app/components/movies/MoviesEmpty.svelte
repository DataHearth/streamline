<script lang="ts">
	import { Clapperboard, SearchX, X } from "@lucide/svelte";

	let { variant, onClear } = $props();
</script>

<div
	class="col-span-full flex w-full flex-col items-center justify-center rounded-lg border border-dashed border-border bg-bg-card/40 py-16 text-center"
>
	{#if variant === "library"}
		<Clapperboard class="mb-3 h-12 w-12 text-fg-faint" aria-hidden="true" />
		<p class="text-base font-semibold text-fg">Your library is empty</p>
		<p class="mt-1 max-w-sm text-sm text-fg-subtle">
			Add a movie from TMDB or import what you already have on disk to get
			started.
		</p>
	{:else}
		<SearchX class="mb-3 h-12 w-12 text-fg-faint" aria-hidden="true" />
		<p class="text-base font-semibold text-fg">
			No movies match this view
		</p>
		<p class="mt-1 max-w-sm text-sm text-fg-subtle">
			Try a different tab or clear the search to see your full library.
		</p>
		<button
			type="button"
			onclick={onClear}
			class="mt-5 inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-card px-3.5 py-2 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
		>
			<X class="h-4 w-4" aria-hidden="true" />
			Clear filters
		</button>
	{/if}
</div>
