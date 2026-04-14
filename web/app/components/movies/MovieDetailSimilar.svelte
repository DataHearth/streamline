<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Film } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import Poster from "./Poster.svelte";
	import AddRecommendationModal from "./AddRecommendationModal.svelte";
	import type {
		MovieRecommendations,
		PaginatedMovies,
		TMDBMovieResult,
	} from "../../lib/types";

	let { movieId }: { movieId: number } = $props();

	const q = createQuery<MovieRecommendations>(() => ({
		queryKey: ["movie", movieId, "recommendations"],
		queryFn: () =>
			api<MovieRecommendations>(`/movies/${movieId}/recommendations`),
		enabled: Number.isFinite(movieId) && movieId > 0,
		retry: false,
	}));

	// Library lookup so an already-added recommendation links straight to
	// its detail page instead of re-opening the add modal.
	const libQuery = createQuery<PaginatedMovies>(() => ({
		queryKey: ["movies"],
		queryFn: () => api<PaginatedMovies>("/movies?page=1&limit=500"),
	}));
	let libraryByTmdb = $derived.by(() => {
		const map = new Map<number, number>();
		for (const m of libQuery.data?.items ?? []) map.set(m.tmdb_id, m.id);
		return map;
	});

	let recs = $derived.by(() => {
		const items = q.data?.items ?? [];
		return items.filter((m) => !!m.poster_url).slice(0, 6);
	});

	let addOpen = $state(false);
	let selected = $state<TMDBMovieResult | null>(null);

	function openAdd(rec: TMDBMovieResult) {
		selected = rec;
		addOpen = true;
	}
</script>

{#if recs.length > 0}
	<section class="min-w-0" aria-labelledby="similar-label">
		<header class="mb-3 flex items-baseline justify-between">
			<h3
				id="similar-label"
				class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
			>
				More like this
			</h3>
		</header>
		<div class="poster-scroll -mx-1 px-1 pb-1">
			{#each recs as rec (rec.tmdb_id)}
				{@const localId = libraryByTmdb.get(rec.tmdb_id)}
				{#snippet poster()}
					<div class="relative aspect-[2/3] w-full bg-bg-card">
						<div
							class="absolute inset-0 grid place-items-center text-fg-faint"
						>
							<Film class="h-9 w-9" aria-hidden="true" />
						</div>
						<Poster
							src={rec.poster_url ?? ""}
							alt="{rec.title} poster"
							class="relative h-full w-full object-cover transition duration-300 group-hover:scale-[1.03] motion-reduce:transition-none motion-reduce:group-hover:scale-100"
						/>
						<div
							class="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/55 to-transparent px-3 pt-12 pb-2.5"
						>
							<p
								class="truncate text-sm font-semibold text-white drop-shadow-[0_1px_3px_rgb(0_0_0_/0.95)]"
								title={rec.title}
							>
								{rec.title}
							</p>
							{#if rec.original_title.trim() && rec.original_title.trim() !== rec.title.trim()}
								<p
									class="truncate text-[11px] italic text-white/70 drop-shadow-[0_1px_2px_rgb(0_0_0_/0.9)]"
									title={rec.original_title}
								>
									{rec.original_title}
								</p>
							{/if}
							{#if rec.year}
								<p
									class="mt-0.5 font-mono text-[11px] tracking-tight text-white/80 drop-shadow-[0_1px_2px_rgb(0_0_0_/0.9)]"
								>
									{rec.year}
								</p>
							{/if}
						</div>
					</div>
				{/snippet}

				{#if localId !== undefined}
					<a
						href="/movies/{localId}"
						class="snap-start group relative block overflow-hidden rounded-lg ring-1 ring-border transition duration-200 hover:ring-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent motion-reduce:transition-none"
						title="In your library — open details"
					>
						{@render poster()}
					</a>
				{:else}
					<button
						type="button"
						onclick={() => openAdd(rec)}
						class="snap-start group relative block w-full overflow-hidden rounded-lg text-left ring-1 ring-border transition duration-200 hover:ring-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent motion-reduce:transition-none"
						title="Add {rec.title} to your library"
					>
						{@render poster()}
					</button>
				{/if}
			{/each}
		</div>
	</section>

	<AddRecommendationModal
		open={addOpen}
		rec={selected}
		onClose={() => (addOpen = false)}
	/>
{/if}

<style>
	.poster-scroll {
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: 160px;
		gap: 14px;
		overflow-x: auto;
		overflow-y: hidden;
		scroll-snap-type: x mandatory;
	}
</style>
