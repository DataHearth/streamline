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
			class="relative mx-auto grid w-full max-w-7xl items-end gap-6 px-4 py-12 md:grid-cols-[340px_1fr] md:gap-12 md:px-8 md:py-20"
		>
			<div
				class="relative mx-auto aspect-[2/3] w-48 overflow-hidden rounded-lg shadow-[0_20px_60px_rgb(0_0_0_/0.55)] md:mx-0 md:w-auto"
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

			<div class="pb-2 text-fg">
				<div
					class="mb-3 inline-flex items-center gap-2 font-mono text-[10.5px] uppercase tracking-[0.18em] text-accent-text"
				>
					<span
						class="h-1.5 w-1.5 rounded-full bg-accent motion-safe:animate-pulse"
						aria-hidden="true"
					></span>
					New · Monitored · Tonight
				</div>

				<h2
					class="mb-3 text-4xl font-bold leading-[1.05] tracking-tight text-fg md:text-[56px]"
				>
					{item.title}
				</h2>

				<div
					class="mb-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
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

				{#if item.overview}
					<p
						class="mb-6 line-clamp-3 max-w-[640px] text-sm text-fg-muted [text-wrap:pretty]"
					>
						{item.overview}
					</p>
				{/if}

				<div class="flex flex-wrap items-center gap-3">
					<a
						href={item.href}
						class="inline-flex h-10 items-center gap-2 rounded-md bg-fg px-4 text-sm font-semibold text-bg-deep transition hover:bg-accent hover:text-fg-on-accent hover:shadow-glow active:scale-[0.97]"
					>
						Open details
						<ArrowRight size={14} aria-hidden="true" />
					</a>
					<div class="flex items-center gap-2">
						<StatusPill status={item.status} variant="translucent" />
						{#if item.fileMeta}
							<span class="font-mono text-[11px] text-fg-subtle">
								{item.fileMeta}
							</span>
						{/if}
					</div>
				</div>
			</div>
		</div>
	</section>
{:else if loading}
	<section class="relative overflow-hidden bg-bg-deep" aria-hidden="true">
		<div
			class="mx-auto grid w-full max-w-7xl items-end gap-6 px-4 py-12 md:grid-cols-[340px_1fr] md:gap-12 md:px-8 md:py-20"
		>
			<div
				class="aspect-[2/3] w-48 rounded-lg bg-bg-elevated motion-safe:animate-pulse md:w-auto"
			></div>
			<div class="flex flex-col gap-4 pb-2">
				<div
					class="h-3 w-40 rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="h-11 w-2/3 rounded-lg bg-bg-elevated motion-safe:animate-pulse md:h-14"
				></div>
				<div
					class="h-3 w-24 rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="mt-2 h-3 w-full max-w-[560px] rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="h-3 w-4/5 max-w-[460px] rounded bg-bg-elevated motion-safe:animate-pulse"
				></div>
				<div
					class="mt-3 h-10 w-36 rounded-md bg-bg-elevated motion-safe:animate-pulse"
				></div>
			</div>
		</div>
	</section>
{:else}
	<section
		class="relative overflow-hidden bg-bg-deep"
		aria-label="No featured media yet"
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
					Your library is waiting
				</h2>
				<p class="mb-6 text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
					Add a movie or series — once it's downloaded and available, it'll be
					featured right here.
				</p>
				<a
					href="/movies"
					class="inline-flex h-10 items-center gap-2 rounded-md bg-fg px-4 text-sm font-semibold text-bg-deep transition hover:bg-accent hover:text-fg-on-accent hover:shadow-glow active:scale-[0.97]"
				>
					Browse library
					<ArrowRight size={14} aria-hidden="true" />
				</a>
			</div>
		</div>
	</section>
{/if}

<style>
	.hero-overlay {
		background-image: linear-gradient(
			180deg,
			transparent 0%,
			rgb(11 11 16 / 0.4) 50%,
			var(--bg-deep) 100%
		);
	}
</style>
