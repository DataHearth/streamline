<script lang="ts">
	import { ChevronLeft, ExternalLink, Film, Star, Tv } from "@lucide/svelte";
	import MovieDetailCast from "../movies/MovieDetailCast.svelte";
	import type { LookupDetail } from "../../lib/types";

	// The right-hand pane of the add/request modals: everything TMDB/TVDB knows
	// about the highlighted result, so the choice can be made without leaving
	// the modal. `item` is the search hit already on screen (title, poster,
	// short overview); `detail` is the lazily-fetched remainder.
	type Item = {
		title: string;
		year?: number;
		poster_url?: string;
		overview?: string;
		// Original title for movies, network for series.
		subtitle?: string;
	};

	let {
		kind,
		item,
		detail,
		loading = false,
		error,
		onBack,
		showTitle = true,
		compact = false,
	}: {
		kind: "movie" | "series";
		item?: Item;
		detail?: LookupDetail;
		loading?: boolean;
		error?: string;
		// Small screens show one column at a time; the panel gets a back affordance.
		onBack?: () => void;
		// Off where the title already sits above the panel (expanded request row).
		showTitle?: boolean;
		// Drops the panel's own padding for hosts that bring their own.
		compact?: boolean;
	} = $props();

	function formatDate(iso?: string): string {
		if (!iso) return "";
		const d = new Date(iso.length === 10 ? `${iso}T00:00:00` : iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleDateString(undefined, {
			day: "numeric",
			month: "short",
			year: "numeric",
		});
	}

	function formatRuntime(min?: number): string {
		if (!min || min <= 0) return "";
		if (min < 60) return `${min} min`;
		return `${Math.floor(min / 60)}h ${String(min % 60).padStart(2, "0")}m`;
	}

	let synopsis = $derived(detail?.overview ?? item?.overview ?? "");
	let cast = $derived(detail?.cast ?? []);
	let dateText = $derived(formatDate(detail?.release_date));
	let runtimeText = $derived(formatRuntime(detail?.runtime));
	let ratingText = $derived(
		detail?.rating && detail.rating > 0 ? detail.rating.toFixed(1) : "",
	);

	type Fact = { label: string; value: string; href?: string };
	let facts = $derived.by(() => {
		const out: Fact[] = [];
		if (!detail) return out;
		if (detail.release_date) {
			out.push({
				label: kind === "movie" ? "Released" : "First aired",
				value: detail.release_date,
			});
		}
		if (detail.runtime) {
			out.push({
				label: kind === "movie" ? "Runtime" : "Episode",
				value: `${detail.runtime}m`,
			});
		}
		if (detail.season_count) {
			out.push({
				label: "Seasons",
				value: `${detail.season_count}`,
			});
		}
		if (detail.episode_count) {
			out.push({ label: "Episodes", value: `${detail.episode_count}` });
		}
		if (detail.status) out.push({ label: "Status", value: detail.status });
		if (detail.network) out.push({ label: "Network", value: detail.network });
		if (detail.original_language) {
			out.push({
				label: "Language",
				value: detail.original_language.toUpperCase(),
			});
		}
		if (detail.rating) {
			out.push({ label: "Rating", value: `${detail.rating.toFixed(1)} / 10` });
		}
		if (detail.vote_count) {
			out.push({
				label: "Votes",
				value: detail.vote_count.toLocaleString(),
			});
		}
		if (detail.tmdb_id) {
			out.push({
				label: "TMDB",
				value: `${detail.tmdb_id}`,
				href: `https://www.themoviedb.org/${kind === "movie" ? "movie" : "tv"}/${detail.tmdb_id}`,
			});
		}
		if (detail.tvdb_id) {
			out.push({
				label: "TVDB",
				value: `${detail.tvdb_id}`,
				href: `https://www.thetvdb.com/dereferrer/series/${detail.tvdb_id}`,
			});
		}
		if (detail.imdb_id) {
			out.push({
				label: "IMDb",
				value: detail.imdb_id,
				href: `https://www.imdb.com/title/${detail.imdb_id}/`,
			});
		}
		return out;
	});
</script>

{#if !item}
	<div
		class="flex h-full flex-col items-center justify-center px-8 py-16 text-center"
	>
		{#if kind === "movie"}
			<Film class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
		{:else}
			<Tv class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
		{/if}
		<p class="text-sm font-medium text-fg-muted">Nothing selected</p>
		<p class="mt-1 text-xs text-fg-faint">
			Pick a result to see its synopsis, cast and IDs.
		</p>
	</div>
{:else}
	<div class="flex flex-col {compact ? 'gap-4' : 'gap-5 px-5 py-5'}">
		{#if onBack}
			<button
				type="button"
				onclick={onBack}
				class="-ml-2 -mt-1 inline-flex w-fit items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-fg-muted transition hover:bg-surface hover:text-fg md:hidden"
			>
				<ChevronLeft size={14} aria-hidden="true" />
				Results
			</button>
		{/if}

		<div class="flex gap-4">
			<div
				class="relative aspect-[2/3] w-[104px] flex-none overflow-hidden rounded-md border border-white/[0.06] bg-bg-card shadow-2"
			>
				<div class="absolute inset-0 grid place-items-center text-fg-faint">
					{#if kind === "movie"}
						<Film class="h-7 w-7" aria-hidden="true" />
					{:else}
						<Tv class="h-7 w-7" aria-hidden="true" />
					{/if}
				</div>
				{#if item.poster_url}
					<img
						src={item.poster_url}
						alt=""
						loading="lazy"
						class="relative h-full w-full object-cover"
					/>
				{/if}
			</div>

			<div class="flex min-w-0 flex-1 flex-col justify-center">
				{#if showTitle}
					<h3
						class="text-xl font-bold leading-tight tracking-tight text-fg [text-wrap:pretty]"
					>
						{item.title}
					</h3>
				{/if}
				{#if item.subtitle}
					<p class="mt-1 truncate text-[12.5px] italic text-fg-faint">
						{item.subtitle}
					</p>
				{/if}
				{#if detail?.tagline}
					<p class="mt-1.5 text-[12.5px] text-fg-subtle [text-wrap:pretty]">
						{detail.tagline}
					</p>
				{/if}

				<div class="flex flex-wrap gap-1.5 {showTitle || item.subtitle || detail?.tagline ? 'mt-3' : ''}">
					{#if ratingText}
						<span
							class="inline-flex h-6 items-center gap-1 rounded-full border border-status-wanted/30 bg-surface px-2.5 font-mono text-[11px] text-status-wanted"
						>
							<Star size={11} aria-hidden="true" />
							{ratingText}
						</span>
					{/if}
					{#if dateText}
						<span
							class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
						>
							{dateText}
						</span>
					{:else if item.year}
						<span
							class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
						>
							{item.year}
						</span>
					{/if}
					{#if runtimeText}
						<span
							class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
						>
							{runtimeText}
						</span>
					{/if}
					{#each detail?.genres ?? [] as g (g)}
						<span
							class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
						>
							{g}
						</span>
					{/each}
					{#if loading}
						<span
							class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-faint"
						>
							loading…
						</span>
					{/if}
				</div>
			</div>
		</div>

		{#if synopsis}
			<section>
				<h4
					class="mb-2 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint"
				>
					Synopsis
				</h4>
				<p class="text-[13px] leading-relaxed text-fg-muted [text-wrap:pretty]">
					{synopsis}
				</p>
			</section>
		{/if}

		{#if error}
			<p
				role="alert"
				class="rounded-md border border-dashed border-status-failed/40 bg-status-failed/5 px-3 py-2.5 text-xs text-status-failed"
			>
				{error}
			</p>
		{:else if loading && !detail}
			<section>
				<h4
					class="mb-2 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint"
				>
					Top billed
				</h4>
				<div class="grid grid-cols-5 gap-3">
					{#each [0, 1, 2, 3, 4] as i (i)}
						<div>
							<div
								class="aspect-square animate-pulse rounded-md bg-bg-card"
							></div>
							<div
								class="mx-auto mt-2 h-2 w-3/4 animate-pulse rounded bg-bg-card"
							></div>
						</div>
					{/each}
				</div>
			</section>
		{:else if cast.length > 0}
			<section>
				<h4
					class="mb-2.5 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint"
				>
					Top billed
				</h4>
				<MovieDetailCast {cast} dense />
			</section>
		{/if}

		{#if facts.length > 0}
			<section>
				<h4
					class="mb-2.5 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint"
				>
					Details
				</h4>
				<dl
					class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px] sm:grid-cols-[auto_1fr_auto_1fr] sm:gap-x-5"
				>
					{#each facts as f (f.label)}
						<dt class="text-fg-subtle">{f.label}</dt>
						<dd class="m-0 text-right font-mono text-fg">
							{#if f.href}
								<a
									href={f.href}
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 text-accent-text transition hover:text-accent"
								>
									{f.value}
									<ExternalLink size={11} aria-hidden="true" />
								</a>
							{:else}
								{f.value}
							{/if}
						</dd>
					{/each}
				</dl>
			</section>
		{/if}
	</div>
{/if}
