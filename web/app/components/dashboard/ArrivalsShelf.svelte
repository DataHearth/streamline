<script lang="ts">
	import type { Snippet } from "svelte";
	import { ChevronLeft, ChevronRight, Sparkles } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Img from "@components/shared/Img.svelte";
	import ArrivalCard from "./ArrivalCard.svelte";
	import { arrivalArt, arrivalTally } from "@lib/dashboard";
	import type { LibraryArrival } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The dashboard's hero: what landed in the library this week, every medium
	// on one shelf at its own shape, over a backdrop blurred out of the newest
	// three. `top` rides above it, inside the backdrop — what is waiting on the
	// person looking, when anything is.
	let {
		arrivals,
		loading = false,
		error = false,
		top,
	}: {
		arrivals: LibraryArrival[];
		loading?: boolean;
		// The arrivals could not be read. The shelf goes rather than claiming
		// nothing arrived; `top` still renders.
		error?: boolean;
		top?: Snippet;
	} = $props();

	const WEEK = 7 * 86_400_000;
	let week = $derived(arrivals.filter((a) => Date.now() - Date.parse(a.at) < WEEK));
	// A quiet week keeps the shelf filled with the latest few, under a title
	// that does not promise a week.
	let shown = $derived(week.length > 0 ? week : arrivals.slice(0, 12));
	let tally = $derived(arrivalTally(week));
	let backdrop = $derived(shown.slice(0, 3).map(arrivalArt));
	let title = $derived(
		week.length > 0 || arrivals.length === 0 ? i18n.dash_new_this_week() : i18n.dash_recently_added(),
	);

	let scrollEl = $state<HTMLDivElement | null>(null);
	let atStart = $state(true);
	let atEnd = $state(true);

	// The row is inset by its own gutter, and scroll-snap parks the first card
	// at that offset, so "far left" is the padding, not 0.
	function updateBounds() {
		if (!scrollEl) return;
		const { scrollLeft, scrollWidth, clientWidth } = scrollEl;
		const cs = getComputedStyle(scrollEl);
		const padL = parseFloat(cs.paddingLeft) || 0;
		const padR = parseFloat(cs.paddingRight) || 0;
		atStart = scrollLeft <= padL + 1;
		atEnd = scrollLeft + clientWidth >= scrollWidth - padR - 1;
	}

	$effect(() => {
		shown;
		if (!scrollEl) return;
		updateBounds();
		const ro = new ResizeObserver(updateBounds);
		ro.observe(scrollEl);
		return () => ro.disconnect();
	});

	// Newest first, so an arrival is prepended and scroll anchoring would park
	// it off the left edge. When the lead card changes, go back to the start.
	let leadKey: string | null = null;
	$effect(() => {
		const lead = shown[0];
		const next = lead ? `${lead.kind}:${lead.id}:${lead.at}` : null;
		const first = leadKey === null;
		if (next === leadKey) return;
		leadKey = next;
		if (first || !scrollEl) return;
		scrollEl.scrollTo({ left: 0, behavior: "smooth" });
	});

	function scrollBy(dir: 1 | -1) {
		scrollEl?.scrollBy({ left: dir * scrollEl.clientWidth * 0.8, behavior: "smooth" });
	}
	const navBtn =
		"grid h-7 w-7 place-items-center rounded-md border border-border bg-bg-deep/40 text-fg-muted transition-colors hover:border-border-strong hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:pointer-events-none disabled:opacity-40";
</script>

<section class="relative isolate" aria-labelledby="arrivals-title">
	{#if !error && backdrop.length > 0}
		<div class="pointer-events-none absolute inset-0 -z-10 overflow-hidden" aria-hidden="true">
			<div class="absolute -inset-x-16 -top-24 bottom-0 flex opacity-40 blur-3xl saturate-150">
				{#each backdrop as src, i (i)}
					<Img {src} alt="" class="h-full min-w-0 flex-1 object-cover" />
				{/each}
			</div>
			<div class="shelf-fade absolute inset-0"></div>
		</div>
	{/if}

	<div class="mx-auto flex w-full max-w-7xl flex-col gap-8 pt-5 md:gap-7 md:pt-6">
		{#if top}
			<div class="px-4 md:px-8">{@render top()}</div>
		{/if}

		{#if !error}
			<div>
				<header class="flex items-end justify-between gap-4 px-4 md:px-8">
					<div class="min-w-0">
						<h2
							id="arrivals-title"
							class="text-[22px] font-bold leading-tight tracking-tight text-fg md:text-[28px]"
						>
							{title}
						</h2>
						{#if loading}
							<div class="mt-2 h-3 w-56 rounded bg-bg-elevated motion-safe:animate-pulse"></div>
						{:else if week.length > 0}
							<p class="mt-1.5 flex flex-wrap items-center gap-x-3.5 gap-y-1 text-[12.5px] text-fg-muted">
								<span class="font-mono text-fg-subtle">{i18n.dash_arrivals_week({ count: week.length })}</span>
								{#each tally as t (t.token)}
									<span class="inline-flex items-center gap-1.5 whitespace-nowrap">
										<span class="h-1.5 w-1.5 rounded-full" style:background="var(--kind-{t.token})" aria-hidden="true"></span>
										{t.label}
									</span>
								{/each}
							</p>
						{/if}
					</div>
					{#if shown.length > 0}
						<div
							class="hidden shrink-0 items-center gap-1 lg:flex"
							role="group"
							aria-label={i18n.dash_scroll_row({ title })}
						>
							<button type="button" class={navBtn} aria-label={i18n.common_scroll_left()} disabled={atStart} onclick={() => scrollBy(-1)}>
								<ChevronLeft size={14} aria-hidden="true" />
							</button>
							<button type="button" class={navBtn} aria-label={i18n.common_scroll_right()} disabled={atEnd} onclick={() => scrollBy(1)}>
								<ChevronRight size={14} aria-hidden="true" />
							</button>
						</div>
					{/if}
				</header>

				{#if loading}
					<div class="shelf-row mt-4 flex gap-4 overflow-hidden px-4 pb-1 pt-2 md:gap-5 md:px-8" aria-hidden="true">
						{#each Array.from({ length: 7 }) as _, i (i)}
							<div class="shrink-0" style:width="var(--card-p)">
								<div class="rounded-lg bg-bg-elevated motion-safe:animate-pulse" style:height="var(--card-h)"></div>
								<div class="mt-2.5 h-3 w-4/5 rounded bg-bg-elevated motion-safe:animate-pulse"></div>
								<div class="mt-2 h-2.5 w-3/5 rounded bg-bg-elevated motion-safe:animate-pulse"></div>
							</div>
						{/each}
					</div>
				{:else if shown.length === 0}
					<div class="px-4 md:px-8">
						<div class="mt-4 flex items-center gap-4 rounded-lg border border-dashed border-border bg-bg-elevated/40 px-5 py-6">
							<Sparkles size={20} class="shrink-0 text-fg-faint" aria-hidden="true" />
							<div class="min-w-0">
								<p class="text-sm font-medium text-fg">{i18n.dash_arrivals_empty()}</p>
								<p class="mt-0.5 text-[13px] text-fg-muted [text-wrap:pretty]">{i18n.dash_arrivals_empty_help()}</p>
							</div>
						</div>
					</div>
				{:else}
					<div
						bind:this={scrollEl}
						onscroll={updateBounds}
						class={cn("shelf-row shelf-scroll relative mt-2 flex gap-4 px-4 pb-2 pt-4 md:gap-5 md:px-8", !atEnd && "fade-end")}
					>
						{#each shown as a (`${a.kind}:${a.id}:${a.at}`)}
							<ArrivalCard arrival={a} />
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>
</section>

<style>
	/* Card sizes per band. 120px on a phone shows two posters and the edge of a
	   third, which is what says the row scrolls; a square album takes the
	   poster's height less a little, so it reads as a record beside a book
	   rather than as a smaller poster. */
	.shelf-row {
		--card-p: 120px;
		--card-sq: 156px;
		--card-h: 180px;
	}
	@media (min-width: 768px) {
		.shelf-row {
			--card-p: 128px;
			--card-sq: 170px;
			--card-h: 192px;
		}
	}
	@media (min-width: 1024px) {
		.shelf-row {
			--card-p: 150px;
			--card-sq: 200px;
			--card-h: 225px;
		}
	}
	.shelf-scroll {
		overflow-x: auto;
		scroll-snap-type: x mandatory;
		scroll-padding-inline: 16px;
		scrollbar-width: none;
	}
	.shelf-scroll::-webkit-scrollbar {
		display: none;
	}
	@media (min-width: 768px) {
		.shelf-scroll {
			scroll-padding-inline: 32px;
		}
	}
	/* Fades the trailing edge while there is more to the right — the row's only
	   affordance below lg, where the arrows are not drawn. */
	.fade-end {
		mask-image: linear-gradient(90deg, #000 calc(100% - 56px), transparent);
	}
	.shelf-fade {
		background-image: linear-gradient(
			180deg,
			rgb(11 11 16 / 0.15) 0%,
			rgb(11 11 16 / 0.6) 55%,
			var(--bg-deep) 100%
		);
	}
</style>
