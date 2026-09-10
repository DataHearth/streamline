<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { params } from "@roxi/routify";
	import { onMount } from "svelte";
	import { UserRound } from "@lucide/svelte";
	import { api, ApiError, errorText } from "../../lib/api";
	import { formatBytes } from "../../lib/format";
	import { initials } from "../../lib/people";
	import { tvPosterUrl } from "../../lib/posters";
	import { movieStatus, seriesStatus } from "../../lib/status";
	import PosterCard from "../../components/shared/PosterCard.svelte";
	import type { Movie, PersonDetail, TVShow } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let routeParams = $state<Record<string, string>>({});
	onMount(() => params.subscribe((p) => (routeParams = p)));
	const personID = $derived(Number(routeParams.id));

	const personQuery = createQuery<PersonDetail>(() => ({
		queryKey: ["person", personID],
		queryFn: () => api<PersonDetail>(`/people/${personID}`),
		enabled: Number.isFinite(personID) && personID > 0,
		// Nobody with credits in the library answers 404, which is an answer and
		// not a failure worth retrying.
		retry: false,
	}));

	let person = $derived(personQuery.data);
	let notFound = $derived(
		personQuery.error instanceof ApiError && personQuery.error.status === 404,
	);
	// One card per title, not per credit: an actor voicing two characters in the
	// same show has two credit rows, which as two cards reads as a duplicate
	// poster — and as an {#each} keyed on the title id is a duplicate key that
	// takes the whole page down. Tom Kenny in Adventure Time is both.
	function roles(characters: string[]): string | undefined {
		return characters.filter(Boolean).join(", ") || undefined;
	}

	let movieCredits = $derived.by(() => {
		const seen = new Map<number, { movie: Movie; characters: string[] }>();
		for (const c of person?.movies ?? []) {
			const entry = seen.get(c.movie.id) ?? { movie: c.movie, characters: [] };
			entry.characters.push(c.character);
			seen.set(c.movie.id, entry);
		}
		return [...seen.values()].map((e) => ({
			movie: e.movie,
			character: roles(e.characters),
		}));
	});

	let seriesCredits = $derived.by(() => {
		const seen = new Map<number, { series: TVShow; characters: string[] }>();
		for (const c of person?.series ?? []) {
			const entry = seen.get(c.series.id) ?? { series: c.series, characters: [] };
			entry.characters.push(c.character);
			seen.set(c.series.id, entry);
		}
		return [...seen.values()].map((e) => ({
			series: e.series,
			character: roles(e.characters),
		}));
	});

	let countParts = $derived.by(() => {
		const p: string[] = [];
		const m = movieCredits.length;
		const s = seriesCredits.length;
		if (m > 0)
			p.push(m === 1 ? i18n.dash_movie_count_one({ count: m }) : i18n.dash_movie_count_other({ count: m }));
		if (s > 0)
			p.push(s === 1 ? i18n.dash_series_count_one({ count: s }) : i18n.dash_series_count_other({ count: s }));
		return p;
	});

	function movieCard(m: Movie) {
		// The credits list carries the same file rollup the library grid reads;
		// media_files is detail-only.
		const f = m.file_summary;
		return {
			id: m.id,
			title: m.title,
			original_title: m.original_title,
			year: m.year,
			releaseDate: m.release_date,
			status: movieStatus(m),
			monitored: m.monitored,
			rating: m.rating,
			resolution: f?.resolution,
			size_text: formatBytes(f?.size_bytes, ""),
		};
	}

	function seriesCard(s: TVShow) {
		return {
			id: s.id,
			title: s.title,
			original_title: s.original_title,
			year: s.year,
			releaseDate: s.first_aired,
			status: seriesStatus(s),
			monitored: s.monitored,
			rating: s.rating ?? undefined,
		};
	}

	const gridClass =
		"grid gap-x-4 gap-y-6 grid-cols-[repeat(auto-fill,minmax(160px,1fr))] md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,1fr))]";
	const headingClass =
		"mb-3 font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";
</script>

{#if personQuery.isLoading}
	<section class="relative overflow-hidden bg-bg-deep">
		<div class="flex w-full items-stretch gap-10 px-4 py-16 md:px-8">
			<div
				class="aspect-square w-[200px] animate-pulse rounded-lg bg-bg-card/60 motion-reduce:animate-none"
			></div>
			<div class="flex flex-1 flex-col gap-3">
				<div
					class="h-8 w-2/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
				<div
					class="h-5 w-1/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
			</div>
		</div>
	</section>
{:else if notFound}
	<div
		class="mx-4 mt-4 rounded-lg border border-dashed border-border bg-bg-elevated/40 py-12 text-center md:mx-8"
	>
		<p class="text-sm font-medium text-fg">{i18n.person_not_found()}</p>
		<p class="mt-1 text-xs text-fg-muted">{i18n.person_not_found_help()}</p>
	</div>
{:else if personQuery.isError}
	<div
		class="mx-4 mt-4 rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center md:mx-8"
	>
		<p class="text-sm font-semibold text-status-failed">
			{i18n.person_load_failed()}
		</p>
		<p class="mt-1 text-xs text-fg-subtle">
			{errorText(personQuery.error, i18n.common_unknown_error())}
		</p>
	</div>
{:else if person}
	<section class="relative" aria-labelledby="person-name">
		<div class="absolute inset-0 z-0 overflow-hidden bg-bg-deep">
			{#if person.profile_url}
				<img
					src={person.profile_url}
					alt=""
					aria-hidden="true"
					class="h-full w-full scale-110 object-cover opacity-70 blur-md"
				/>
			{/if}
			<div class="absolute inset-0 hero-overlay"></div>
		</div>

		<div
			class="relative grid w-full items-end gap-5 px-4 pb-7 pt-8 md:grid-cols-[200px_1fr] md:gap-8 md:px-8 md:pb-12 md:pt-10 lg:grid-cols-[260px_1fr] lg:gap-10"
		>
			<div
				class="relative mx-auto aspect-square w-44 overflow-hidden rounded-lg bg-bg-card shadow-[0_24px_48px_rgb(0_0_0_/0.5)] md:mx-0 md:w-auto"
			>
				{#if person.profile_url}
					<img
						src={person.profile_url}
						alt={person.name}
						class="h-full w-full object-cover"
					/>
				{:else}
					<span
						class="grid h-full w-full place-items-center font-mono text-4xl font-bold text-fg-faint md:text-5xl"
					>
						{initials(person.name)}
					</span>
				{/if}
			</div>

			<div class="min-w-0 text-left">
				<div
					class="mb-3 inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-2.5 py-1 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-muted backdrop-blur-sm"
				>
					<UserRound size={12} aria-hidden="true" />
					{i18n.common_person()}
				</div>

				<h1
					id="person-name"
					class="text-[26px] font-bold leading-[1.05] tracking-tight text-fg md:text-4xl lg:text-5xl"
					title={person.name}
				>
					{person.name}
				</h1>

				{#if countParts.length > 0}
					<div
						class="mt-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
					>
						{#each countParts as part, i (i)}
							{#if i > 0}
								<span class="text-fg-faint" aria-hidden="true">·</span>
							{/if}
							<span>{part}</span>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</section>

	<div class="w-full space-y-8 px-4 pb-8 pt-6 md:px-8">
		{#if movieCredits.length > 0}
			<section aria-labelledby="person-movies">
				<h2 id="person-movies" class={headingClass}>{i18n.movies_label()}</h2>
				<div class={gridClass}>
					{#each movieCredits as credit (credit.movie.id)}
						<PosterCard
							movie={movieCard(credit.movie)}
							detail={credit.character || undefined}
						/>
					{/each}
				</div>
			</section>
		{/if}

		{#if seriesCredits.length > 0}
			<section aria-labelledby="person-series">
				<h2 id="person-series" class={headingClass}>{i18n.settings_series()}</h2>
				<div class={gridClass}>
					{#each seriesCredits as credit (credit.series.id)}
						<PosterCard
							movie={seriesCard(credit.series)}
							href={`/series/${credit.series.id}`}
							posterSrc={tvPosterUrl(credit.series.id)}
							detail={credit.character || undefined}
						/>
					{/each}
				</div>
			</section>
		{/if}

		<!-- The endpoint 404s a person with no credits, so this only shows if one
		     answers 200 with both lists empty. -->
		{#if movieCredits.length === 0 && seriesCredits.length === 0}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-elevated/40 py-10 text-center"
			>
				<p class="text-sm font-medium text-fg">{i18n.person_no_credits()}</p>
				<p class="mt-1 text-xs text-fg-muted">{i18n.person_not_found_help()}</p>
			</div>
		{/if}
	</div>
{/if}

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
