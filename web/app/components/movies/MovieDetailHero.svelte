<script lang="ts">
	import type { Snippet } from "svelte";
	import { Film, ArrowLeft } from "@lucide/svelte";
	import { posterUrl } from "../../lib/posters";
	import { movieStatus } from "../../lib/status";
	import Poster from "./Poster.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import type { Movie, MediaFile } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		movie,
		actions,
	}: {
		movie: Movie;
		actions?: Snippet;
	} = $props();

	function pickPrimary(
		files: MediaFile[] | undefined,
	): MediaFile | undefined {
		if (!files || files.length === 0) return undefined;
		return [...files].sort((a, b) => b.size - a.size)[0];
	}

	let primary = $derived(pickPrimary(movie.media_files));
	let backdropSrc = $derived(posterUrl(movie));

	// Single dotted meta line: year · runtime · rating · genres, then any
	// file-derived facts. Mirrors the prototype's left-aligned header.
	let metaParts = $derived.by(() => {
		const p: string[] = [];
		if (movie.year) p.push(String(movie.year));
		if (movie.runtime) p.push(`${movie.runtime}m`);
		if (movie.rating) p.push(`★ ${movie.rating.toFixed(1)}`);
		if (movie.genres?.length) p.push(movie.genres.join(" / "));
		if (primary?.parsed_resolution) p.push(primary.parsed_resolution);
		if (primary?.parsed_codec) p.push(primary.parsed_codec);
		if (primary?.parsed_source) p.push(primary.parsed_source);
		return p;
	});
</script>

<section
	class="hero relative"
	aria-labelledby="movie-title"
>
	<div class="absolute inset-0 z-0 overflow-hidden bg-bg-deep">
		<img
			src={backdropSrc}
			alt=""
			aria-hidden="true"
			class="h-full w-full scale-110 object-cover opacity-70 blur-md"
		/>
		<div class="absolute inset-0 hero-overlay"></div>
	</div>

	<div class="relative w-full px-4 pt-6 md:px-8">
		<a
			href="/movies"
			class="inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-3 py-1.5 text-[11.5px] font-medium text-fg-muted backdrop-blur-sm transition hover:bg-black/60 hover:text-fg"
		>
			<ArrowLeft size={13} aria-hidden="true" />
			{i18n.movies_label()}
		</a>
	</div>

	<div
		class="relative grid w-full items-end gap-5 px-4 pb-7 pt-6 md:grid-cols-[200px_1fr] md:gap-8 md:px-8 md:pb-16 md:pt-10 lg:grid-cols-[260px_1fr] lg:gap-10 lg:pb-20"
	>
		<div
			class="relative mx-auto aspect-[2/3] w-60 overflow-hidden rounded-lg shadow-[0_24px_48px_rgb(0_0_0_/0.5)] md:mx-0 md:w-auto"
		>
			<div class="absolute inset-0 bg-bg-card"></div>
			<div class="absolute inset-0 grid place-items-center text-fg-faint">
				<Film class="h-10 w-10" aria-hidden="true" />
			</div>
			<Poster
				src={backdropSrc}
				alt="{movie.title} poster"
				loading="eager"
				class="relative h-full w-full object-cover"
			/>
		</div>

		<div class="min-w-0 text-left">
			<div class="mb-3 flex flex-wrap items-center gap-2">
				<StatusPill
					status={movieStatus(movie)}
					size="md"
					live={movie.status === "downloading"}
					variant="translucent"
				/>
			</div>

			<h1
				id="movie-title"
				class="text-[26px] font-bold leading-[1.05] tracking-tight text-fg md:text-4xl lg:text-5xl"
				title={movie.title}
			>
				{movie.title}
			</h1>

			{#if movie.original_title.trim() && movie.original_title.trim() !== movie.title.trim()}
				<p class="mt-1 text-sm italic text-fg-faint">
					{movie.original_title}
				</p>
			{/if}

			{#if metaParts.length > 0}
				<div
					class="mt-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
				>
					{#each metaParts as part, i (i)}
						{#if i > 0}
							<span class="text-fg-faint" aria-hidden="true">·</span>
						{/if}
						<span>{part}</span>
					{/each}
				</div>
			{/if}

			{#if movie.overview}
				<p
					class="mt-4 line-clamp-3 max-w-[680px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
				>
					{movie.overview}
				</p>
			{/if}

			{#if actions}
				<!-- md and up only: on a phone these live in the pinned bar at the
				     bottom of the page, where Manual search is reachable from anywhere
				     in the document rather than only from the top of it. -->
				<div
					class="mt-5 hidden flex-wrap items-center gap-2.5 md:flex"
					aria-label={i18n.movies_actions()}
				>
					{@render actions()}
				</div>
			{/if}
		</div>
	</div>
</section>

<style>
	.hero-overlay {
		background-image: linear-gradient(
			180deg,
			rgb(11 11 16 / 0.3) 0%,
			rgb(11 11 16 / 0.7) 60%,
			var(--bg-deep) 100%
		);
	}
</style>
