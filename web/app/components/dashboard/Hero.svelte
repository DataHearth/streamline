<script lang="ts" module>
	import type { StatusKind } from "../shared/StatusPill.svelte";

	// A movie or series normalized for the hero. The dashboard builds it so the
	// hero itself stays media-agnostic.
	export type HeroItem = {
		title: string;
		year: number;
		overview?: string;
		runtime?: number;
		rating?: number | null;
		status: StatusKind;
		resolution?: string;
		codec?: string;
		fileMeta?: string;
		posterSrc: string;
		href: string;
	};
</script>

<script lang="ts">
	import { ArrowRight, Film } from "@lucide/svelte";
	import Poster from "../movies/Poster.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { item, loading = false }: { item?: HeroItem; loading?: boolean } =
		$props();

	let ratingText = $derived(
		item?.rating && item.rating > 0 ? item.rating.toFixed(1) : "",
	);
</script>

{#if item}
	<section class="hero relative isolate overflow-hidden">
		<div class="absolute inset-0 -z-10 bg-bg-deep">
			<img
				src={item.posterSrc}
				alt=""
				aria-hidden="true"
				class="h-full w-full scale-110 object-cover opacity-70 blur-md"
			/>
			<div class="absolute inset-0 hero-overlay"></div>
		</div>

		<div
			class="hero-grid relative mx-auto w-full max-w-7xl px-4 pb-6 pt-6 md:px-8 md:pb-12 md:pt-14 lg:pt-20"
		>
			<div
				class="relative aspect-[2/3] w-full self-start overflow-hidden rounded-lg shadow-[0_20px_60px_rgb(0_0_0_/0.55)] md:self-end"
				style="grid-area:poster"
			>
				<div class="absolute inset-0 bg-bg-card"></div>
				<div class="absolute inset-0 grid place-items-center text-fg-faint">
					<Film class="h-10 w-10" aria-hidden="true" />
				</div>
				<Poster
					src={item.posterSrc}
					alt="{item.title} poster"
					loading="eager"
					class="relative h-full w-full object-cover"
				/>
			</div>

			<div class="text-fg" style="grid-area:head">
				<div
					class="mb-3 inline-flex max-w-full flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10.5px] uppercase tracking-[0.18em] text-accent-text"
				>
					<span
						class="h-1.5 w-1.5 rounded-full bg-accent motion-safe:animate-pulse"
						aria-hidden="true"
					></span>
					{i18n.dash_hero_badge()}
				</div>

				<h2
					class="mb-3 text-[26px] font-bold leading-[1.08] tracking-tight text-fg md:text-[40px] md:leading-[1.05] lg:text-[56px]"
				>
					{item.title}
				</h2>

				<div
					class="flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
				>
					<span>{item.year}</span>
					{#if item.runtime}
						<span class="text-fg-faint">·</span>
						<span>{item.runtime}m</span>
					{/if}
					{#if ratingText}
						<span class="text-fg-faint">·</span>
						<span>★ {ratingText}</span>
					{/if}
					{#if item.resolution}
						<span class="text-fg-faint">·</span>
						<span>{item.resolution}</span>
					{/if}
					{#if item.codec}
						<span class="text-fg-faint">·</span>
						<span>{item.codec}</span>
					{/if}
				</div>
			</div>

			{#if item.overview}
				<p
					class="line-clamp-2 max-w-[640px] text-sm text-fg-muted [text-wrap:pretty] md:line-clamp-3"
					style="grid-area:body"
				>
					{item.overview}
				</p>
			{/if}

			<div
				class="flex flex-wrap items-center gap-x-3 gap-y-4"
				style="grid-area:actions"
			>
				<a
					href={item.href}
					class="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-fg px-4 text-sm font-semibold text-bg-deep transition hover:bg-accent hover:text-fg-on-accent hover:shadow-glow active:scale-[0.97] md:w-auto md:justify-start"
				>
					{i18n.common_open_details()}
					<ArrowRight size={14} aria-hidden="true" />
				</a>
				<!-- Own row below md. The design stacks these, but only because its
				     fileMeta happens to be long enough to force the wrap — an item
				     without one would sit beside the button instead. -->
				<div class="flex basis-full items-center gap-2 md:basis-auto">
					<StatusPill status={item.status} variant="translucent" />
					{#if item.fileMeta}
						<span class="font-mono text-[11px] text-fg-subtle">
							{item.fileMeta}
						</span>
					{/if}
				</div>
			</div>
		</div>
	</section>
{:else if loading}
	<section class="relative overflow-hidden bg-bg-deep" aria-hidden="true">
		<div
			class="hero-grid mx-auto w-full max-w-7xl px-4 py-6 md:px-8 md:py-14 lg:py-20"
		>
			<div
				class="aspect-[2/3] w-full self-start rounded-lg bg-bg-elevated motion-safe:animate-pulse md:self-end"
				style="grid-area:poster"
			></div>
			<div class="flex flex-col gap-3" style="grid-area:head">
				<div
					class="h-3 w-40 rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="h-8 w-2/3 rounded-lg bg-bg-elevated motion-safe:animate-pulse md:h-11 lg:h-14"
				></div>
				<div
					class="h-3 w-24 rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
			</div>
			<div class="flex flex-col gap-3" style="grid-area:body">
				<div
					class="h-3 w-full max-w-[560px] rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="h-3 w-4/5 max-w-[460px] rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
			</div>
			<div
				class="h-10 w-full rounded-md bg-bg-elevated motion-safe:animate-pulse md:w-36"
				style="grid-area:actions"
			></div>
		</div>
	</section>
{:else}
	<section
		class="relative overflow-hidden bg-bg-deep"
		aria-label={i18n.dash_featured_none()}
	>
		<div class="mx-auto w-full max-w-7xl px-4 py-16 md:px-8 md:py-24">
			<div class="max-w-md">
				<div
					class="mb-5 grid h-14 w-14 place-items-center rounded-2xl border border-border bg-bg-elevated text-fg-faint"
				>
					<Film class="h-7 w-7" aria-hidden="true" />
				</div>
				<h2
					class="mb-2 text-2xl font-bold tracking-tight text-fg md:text-3xl"
				>
					{i18n.dash_library_waiting()}
				</h2>
				<p class="mb-6 text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
					{i18n.dash_hero_empty_help()}
				</p>
				<a
					href="/movies"
					class="inline-flex h-10 items-center gap-2 rounded-md bg-fg px-4 text-sm font-semibold text-bg-deep transition hover:bg-accent hover:text-fg-on-accent hover:shadow-glow active:scale-[0.97]"
				>
					{i18n.dash_browse_library()}
					<ArrowRight size={14} aria-hidden="true" />
				</a>
			</div>
		</div>
	</section>
{/if}

<style>
	/* Phone: the poster sits beside the heading and the copy runs full width
	   under both, so the hero costs ~300px instead of a whole viewport. From md
	   the poster takes its own column beside everything, and the track widens
	   again at lg — a 340px poster in the tablet band left the words 294px. */
	.hero-grid {
		display: grid;
		align-items: start;
		grid-template-columns: 104px 1fr;
		grid-template-areas:
			"poster head"
			"body body"
			"actions actions";
		column-gap: 14px;
		row-gap: 14px;
	}
	/* The poster is taller than the words beside it. Left to auto rows, grid
	   hands that surplus to every row the poster spans, which opened ~100px of
	   dead space above and below the synopsis. A flexible spacer row takes the
	   whole surplus instead, so head/body/actions keep their own 16px rhythm and
	   the block starts at the top of the poster. */
	@media (min-width: 768px) {
		.hero-grid {
			align-items: end;
			grid-template-columns: 200px 1fr;
			grid-template-areas:
				"poster head"
				"poster body"
				"poster actions"
				"poster .";
			grid-template-rows: max-content max-content max-content 1fr;
			column-gap: 32px;
			row-gap: 16px;
		}
	}
	@media (min-width: 1024px) {
		.hero-grid {
			grid-template-columns: 240px 1fr;
			column-gap: 44px;
		}
	}

	.hero-overlay {
		background-image: linear-gradient(
			180deg,
			transparent 0%,
			rgb(11 11 16 / 0.4) 50%,
			var(--bg-deep) 100%
		);
	}
</style>
