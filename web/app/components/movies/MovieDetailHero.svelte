<script lang="ts">
	import type { Snippet } from "svelte";
	import { Film, ArrowLeft } from "@lucide/svelte";
	import { posterUrl } from "@lib/posters";
	import { listHref, MOVIES_SEARCH } from "@lib/prefs";
	import { movieStatus } from "@lib/status";
	import { formatDate } from "@lib/dates";
	import { formatRating } from "@lib/format";
	import Poster from "@components/shared/Poster.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import type { Movie, MediaFile } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import Img from "@components/shared/Img.svelte";
	import ExternalLinks from "@components/shared/ExternalLinks.svelte";
	import { movieLinks } from "@lib/external-links";

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
	let links = $derived(movieLinks(movie));

	// Single dotted meta line: release · runtime · rating · genres, then any
	// file-derived facts. Mirrors the prototype's left-aligned header.
	let metaParts = $derived.by(() => {
		const p: string[] = [];
		// The full date when we have one — it carries the year, so the two never
		// both appear. A title announced without a date still has its year.
		const released = formatDate(movie.release_date);
		if (released) p.push(released);
		else if (movie.year) p.push(String(movie.year));
		const digital = formatDate(movie.digital_release_date);
		if (digital) p.push(i18n.detail_digital_release_on({ date: digital }));
		if (movie.runtime) p.push(`${movie.runtime}m`);
		const rating = formatRating(movie.rating, movie.vote_count);
		if (rating) p.push(rating);
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
		<Img
			src={backdropSrc}
			alt=""
			aria-hidden="true"
			class="h-full w-full scale-110 object-cover opacity-70 blur-md"
		/>
		<div class="absolute inset-0 hero-overlay"></div>
	</div>

	<div class="relative w-full px-4 pt-6 md:px-8">
		<a
			href={listHref("/movies", MOVIES_SEARCH)}
			class="touch-hit inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-3 py-1.5 text-[11.5px] font-medium text-fg-muted backdrop-blur-sm transition hover:bg-black/60 hover:text-fg"
		>
			<ArrowLeft size={13} aria-hidden="true" />
			{i18n.movies_label()}
		</a>
	</div>

	<div
		class="relative grid w-full items-end gap-5 px-4 pb-7 pt-6 md:grid-cols-[200px_1fr] md:gap-8 md:px-8 md:pb-16 md:pt-10 lg:grid-cols-[260px_1fr] lg:gap-10 lg:pb-20"
	>
		<div class="relative mx-auto w-60 md:mx-0 md:w-auto">
			<div
				class="relative aspect-[2/3] w-full overflow-hidden rounded-lg shadow-[0_24px_48px_rgb(0_0_0_/0.5)]"
			>
				<div class="absolute inset-0 bg-bg-card"></div>
				<div class="absolute inset-0 grid place-items-center text-fg-faint">
					<Film class="h-10 w-10" aria-hidden="true" />
				</div>
				<Poster
					src={backdropSrc}
					alt={i18n.common_poster_alt({ title: movie.title })}
					loading="eager"
					class="relative h-full w-full object-cover"
				/>
			</div>
			{#if movieStatus(movie) !== "available"}
				<span class="absolute bottom-0 left-1/2 -translate-x-1/2 translate-y-1/2">
					<StatusPill status={movieStatus(movie)} live={movie.status === "downloading"} />
				</span>
			{/if}
		</div>

		<div class="min-w-0 text-left">
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
				<!-- Each part carries its dot in front, and the row is pulled left by
				     one dot inside a clip: the part that starts a line has its dot
				     clipped, so a wrap never leaves one hanging. -->
				<div class="mt-3 overflow-hidden font-mono text-xs text-fg-muted">
					<div class="-ml-5 flex flex-wrap gap-y-1">
						{#each metaParts as part, i (i)}
							<span class="relative pl-5 before:absolute before:left-0 before:w-5 before:text-center before:text-fg-faint before:content-['·']">{part}</span>
						{/each}
					</div>
				</div>
			{/if}

			{#if movie.overview}
				<p
					class="mt-4 line-clamp-3 max-w-[680px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
				>
					{movie.overview}
				</p>
			{/if}

			<ExternalLinks {links} class="mt-4" />

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
