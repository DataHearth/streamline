<script lang="ts">
	import { Film, ChevronLeft, ChevronRight } from "@lucide/svelte";
	import PosterCard from "../shared/PosterCard.svelte";
	import type { Movie } from "../../lib/types";

	let {
		title,
		movies,
		seeAllHref,
		seeAllLabel = "See all",
		countText,
		emptyText = "No movies yet.",
		pillVariant = "translucent",
	}: {
		title: string;
		movies: Movie[];
		seeAllHref?: string;
		seeAllLabel?: string;
		countText?: string;
		emptyText?: string;
		pillVariant?: "solid" | "translucent";
	} = $props();

	let scrollEl = $state<HTMLDivElement | null>(null);
	let atStart = $state(true);
	let atEnd = $state(true);

	function updateBounds() {
		if (!scrollEl) return;
		const { scrollLeft, scrollWidth, clientWidth } = scrollEl;
		atStart = scrollLeft <= 1;
		atEnd = scrollLeft + clientWidth >= scrollWidth - 1;
	}

	$effect(() => {
		movies; // recompute when the row content changes
		if (!scrollEl) return;
		updateBounds();
		const ro = new ResizeObserver(updateBounds);
		ro.observe(scrollEl);
		return () => ro.disconnect();
	});

	function scrollBy(dir: 1 | -1) {
		if (!scrollEl) return;
		scrollEl.scrollBy({
			left: dir * scrollEl.clientWidth * 0.82,
			behavior: "smooth",
		});
	}
	const navBtn =
		"grid h-7 w-7 place-items-center rounded-md border border-border text-fg-muted transition-colors hover:border-border-strong hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:pointer-events-none disabled:opacity-40";
</script>

<section class="min-w-0">
	<header class="mb-3.5 flex items-baseline justify-between gap-3">
		<h3 class="text-base font-semibold tracking-tight text-fg">{title}</h3>
		<div class="flex items-center gap-3">
			{#if movies.length > 0}
				<div
					class="flex items-center gap-1"
					role="group"
					aria-label="Scroll {title} row"
				>
					<button
						type="button"
						class={navBtn}
						aria-label="Scroll left"
						disabled={atStart}
						onclick={() => scrollBy(-1)}
					>
						<ChevronLeft size={14} aria-hidden="true" />
					</button>
					<button
						type="button"
						class={navBtn}
						aria-label="Scroll right"
						disabled={atEnd}
						onclick={() => scrollBy(1)}
					>
						<ChevronRight size={14} aria-hidden="true" />
					</button>
				</div>
			{/if}
			{#if countText}
				<span class="font-mono text-[11.5px] text-fg-subtle">
					{countText}
				</span>
			{:else if seeAllHref}
				<a
					href={seeAllHref}
					class="text-[11.5px] text-fg-subtle transition hover:text-accent-text"
				>
					{seeAllLabel} →
				</a>
			{/if}
		</div>
	</header>

	{#if movies.length === 0}
		<div
			class="flex items-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated/40 px-5 py-6 text-fg-muted"
		>
			<Film size={18} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm">{emptyText}</p>
		</div>
	{:else}
		<div
			bind:this={scrollEl}
			onscroll={updateBounds}
			class="poster-scroll -mx-1 px-1 pb-1.5"
		>
			{#each movies as movie (movie.id)}
				<div class="snap-start">
					<PosterCard
						movie={{
							id: movie.id,
							title: movie.title,
							year: movie.year,
							status: movie.status,
						}}
						size="md"
						{pillVariant}
					/>
				</div>
			{/each}
		</div>
	{/if}
</section>

<style>
	.poster-scroll {
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: 200px;
		gap: 16px;
		overflow-x: auto;
		scroll-snap-type: x mandatory;
	}
</style>
