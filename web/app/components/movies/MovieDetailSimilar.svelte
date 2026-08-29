<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Film, ChevronLeft, ChevronRight } from "@lucide/svelte";
	import { api, apiAllPages, type Paginated } from "../../lib/api";
	import Poster from "./Poster.svelte";
	import AddRecommendationModal from "./AddRecommendationModal.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import type {
		Movie,
		MovieRecommendations,
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
	const libQuery = createQuery<Paginated<Movie>>(() => ({
		queryKey: ["movies"],
		queryFn: () => apiAllPages<Movie>("/movies"),
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

	let scrollEl = $state<HTMLDivElement | null>(null);
	let atStart = $state(true);
	let atEnd = $state(true);

	function updateBounds() {
		if (!scrollEl) return;
		const { scrollLeft, scrollWidth, clientWidth } = scrollEl;
		// The row is inset by its own p-2 gutter, and scroll-snap parks the first
		// poster at that offset — a "far left" row rests at scrollLeft 8, not 0.
		const cs = getComputedStyle(scrollEl);
		const padL = parseFloat(cs.paddingLeft) || 0;
		const padR = parseFloat(cs.paddingRight) || 0;
		atStart = scrollLeft <= padL + 1;
		atEnd = scrollLeft + clientWidth >= scrollWidth - padR - 1;
	}

	$effect(() => {
		recs;
		if (!scrollEl) return;
		updateBounds();
		const ro = new ResizeObserver(updateBounds);
		ro.observe(scrollEl);
		return () => ro.disconnect();
	});

	function page(dir: 1 | -1) {
		if (!scrollEl) return;
		scrollEl.scrollBy({
			left: dir * scrollEl.clientWidth * 0.82,
			behavior: "smooth",
		});
	}

	const navBtn =
		"hidden h-7 w-7 place-items-center rounded-md border border-border lg:grid text-fg-muted transition-colors hover:border-border-strong hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:pointer-events-none disabled:opacity-40";
</script>

{#if recs.length > 0}
	<section class="min-w-0" aria-labelledby="similar-label">
		<header class="mb-3 flex items-baseline justify-between gap-3">
			<h3
				id="similar-label"
				class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
			>
				{i18n.movies_more_like_this()}
			</h3>
			<div
				class="hidden items-center gap-1 md:flex"
				role="group"
				aria-label={i18n.movies_more_like_this()}
			>
				<button
					type="button"
					class={navBtn}
					aria-label={i18n.common_scroll_left()}
					disabled={atStart}
					onclick={() => page(-1)}
				>
					<ChevronLeft size={14} aria-hidden="true" />
				</button>
				<button
					type="button"
					class={navBtn}
					aria-label={i18n.common_scroll_right()}
					disabled={atEnd}
					onclick={() => page(1)}
				>
					<ChevronRight size={14} aria-hidden="true" />
				</button>
			</div>
		</header>
		<div
			bind:this={scrollEl}
			onscroll={updateBounds}
			class="poster-scroll -m-2 p-2"
		>
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
						title={i18n.movies_in_library_open()}
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
		/* A scroll container clips its cross axis whatever this says, so the
		   hovered card's growth lives in the track's padding instead. */
		overflow-y: hidden;
		scroll-snap-type: x mandatory;
		/* The ‹ › buttons are the affordance; the native bar just adds a rule
		   under the posters. */
		scrollbar-width: none;
	}
	.poster-scroll::-webkit-scrollbar {
		display: none;
	}
</style>
