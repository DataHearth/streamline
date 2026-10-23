<script lang="ts">
	import { BookOpen, Film, Headphones, Tv } from "@lucide/svelte";
	import Poster from "@components/shared/Poster.svelte";
	import ReleaseCover from "@components/music/ReleaseCover.svelte";
	import BookCover from "@components/books/BookCover.svelte";
	import { arrivalArt, arrivalHref, arrivalLines, kindToken } from "@lib/dashboard";
	import { formatArrival } from "@lib/dates";
	import { formatLabel } from "@lib/music-books";
	import type { LibraryArrival } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One arrival on the dashboard shelf, at its medium's own shape: square for
	// an album, 2:3 for the rest. A volume keeps the two sheets the books shelf
	// stacks behind a series. Every card's art box is a poster's height, so a
	// square cover sits on the same baseline and the captions line up. The
	// widths come from the shelf (--card-p, --card-sq, --card-h).
	let { arrival: a }: { arrival: LibraryArrival } = $props();

	let lines = $derived(arrivalLines(a));
	let src = $derived(arrivalArt(a));
	let alt = $derived(i18n.common_poster_alt({ title: a.title }));
	let when = $derived(formatArrival(a.at));
</script>

<a
	href={arrivalHref(a)}
	class="arrival group/card block shrink-0 snap-start rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring focus-visible:ring-offset-4 focus-visible:ring-offset-bg-deep"
	class:square={a.kind === "album"}
	title={a.title}
>
	<div class="art flex items-end">
		<div class="relative w-full transition duration-200 group-hover/card:-translate-y-0.5 group-hover/card:brightness-110">
			{#if a.kind === "album"}
				<ReleaseCover {src} {alt} class="rounded-md shadow-[0_12px_32px_rgb(0_0_0_/0.45)]" />
			{:else if a.kind === "book"}
				{#if a.series_id}
					<div class="absolute inset-0 translate-x-[6px] -translate-y-[6px] rounded-lg border border-white/10 bg-bg-hover" aria-hidden="true"></div>
					<div class="absolute inset-0 translate-x-[3px] -translate-y-[3px] rounded-lg border border-white/10 bg-bg-card" aria-hidden="true"></div>
				{/if}
				<div class="relative">
					<BookCover {src} {alt} class="rounded-lg shadow-[0_12px_32px_rgb(0_0_0_/0.45)]" />
				</div>
			{:else}
				<div class="relative aspect-[2/3] w-full overflow-hidden rounded-lg shadow-[0_12px_32px_rgb(0_0_0_/0.45)]">
					<div class="absolute inset-0 grid place-items-center bg-bg-card text-fg-faint">
						{#if a.kind === "series"}
							<Tv class="h-1/5 w-1/5" aria-hidden="true" />
						{:else}
							<Film class="h-1/5 w-1/5" aria-hidden="true" />
						{/if}
					</div>
					<Poster {src} {alt} class="relative h-full w-full object-cover" />
				</div>
			{/if}
		</div>
	</div>
	<p class="mt-2.5 truncate text-[13px] font-semibold text-fg">{a.title}</p>
	{#if lines.sub}
		<p class="truncate text-[12px] text-fg-muted">{lines.sub}</p>
	{/if}
	<p class="mt-1 flex min-w-0 items-center gap-1 font-mono text-[10.5px] text-fg-subtle">
		<span
			class="mr-0.5 h-1.5 w-1.5 shrink-0 rounded-full"
			style:background="var(--kind-{kindToken(a)})"
			aria-hidden="true"
		></span>
		{#if a.kind === "book" && a.format && a.volume == null}
			<span class="inline-flex shrink-0 text-fg-muted" title={formatLabel(a.format)}>
				{#if a.format === "audiobook"}
					<Headphones size={12} aria-hidden="true" />
				{:else}
					<BookOpen size={12} aria-hidden="true" />
				{/if}
				<span class="sr-only">{formatLabel(a.format)}</span>
			</span>
			{#if when}<span class="shrink-0" aria-hidden="true">·</span>{/if}
		{:else if lines.kind}
			<span class="min-w-0 truncate">{lines.kind}</span>
			{#if when}<span class="shrink-0" aria-hidden="true">·</span>{/if}
		{/if}
		{#if when}
			<span class="shrink-0 whitespace-nowrap">{when}</span>
		{/if}
	</p>
</a>

<style>
	.arrival {
		width: var(--card-p, 120px);
	}
	.arrival.square {
		width: var(--card-sq, 156px);
	}
	.art {
		height: var(--card-h, 180px);
	}
</style>
