<script lang="ts">
	import { cn } from "@lib/cn";
	import BookCover from "./BookCover.svelte";

	// A series has no art of its own: its first volumes fan out behind each
	// other, the front one upright. Sized by its container like BookCover; one
	// poster is a plain cover and none is BookCover's placeholder. A poster that
	// fails leaves BookCover's placeholder tile in its sheet.
	let {
		srcs = [],
		alt = "",
		loading = "lazy",
		rounded = "rounded-lg",
		class: klass = "",
	}: {
		srcs?: string[];
		alt?: string;
		loading?: "eager" | "lazy";
		rounded?: string;
		class?: string;
	} = $props();

	let front = $derived(srcs.slice(0, 3));
	// Drawn back to front, so the front cover paints last.
	let sheets = $derived(front.map((src, depth) => ({ src, depth })).reverse());
</script>

<div class={cn("relative aspect-[2/3] w-full", klass)} data-series-cover>
	{#if front.length <= 1}
		<div data-cover-sheet class={cn("shadow-[0_4px_14px_rgb(0_0_0_/0.45)]", rounded)}>
			<BookCover src={front[0]} {alt} {loading} class={rounded} />
		</div>
	{:else}
		{#each sheets as s (s.depth)}
			<div
				data-cover-sheet
				class={cn("sheet shadow-[0_4px_14px_rgb(0_0_0_/0.45)]", rounded)}
				style:--depth={s.depth}
			>
				<BookCover src={s.src} alt={s.depth === 0 ? alt : ""} {loading} class={rounded} />
			</div>
		{/each}
	{/if}
</div>

<style>
	/* 72% wide, inset 4%, leaves room for two sheets shifted 10% of their width
	   and turned 4° each about their centre: the back one's corners stay inside
	   the box, so a card's overflow clip never cuts the fan. */
	.sheet {
		position: absolute;
		bottom: 3%;
		left: 4%;
		width: 72%;
		transform: translate(calc(var(--depth) * 10%), calc(var(--depth) * -3%)) rotate(calc(var(--depth) * 4deg));
		filter: brightness(calc(1 - var(--depth) * 0.25));
	}
</style>
