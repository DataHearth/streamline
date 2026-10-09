<script lang="ts">
	import { ArrowRight, ChevronLeft, ChevronRight } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { dragScroll } from "@lib/drag-scroll";
	import ShelfCard from "./ShelfCard.svelte";
	import type { ShelfItem } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One row of covers. From md, arrows appear only while the row overflows;
	// below md it is a plain swipe. "See all" opens the kind as a full grid.
	let {
		title,
		items,
		seeAllHref,
		onSeeAll,
	}: { title: string; items: ShelfItem[]; seeAllHref?: string; onSeeAll?: () => void } = $props();

	let scroller = $state<HTMLDivElement | null>(null);
	let canPrev = $state(false);
	let canNext = $state(false);
	function sync() {
		const el = scroller;
		if (!el) return;
		canPrev = el.scrollLeft > 4;
		canNext = el.scrollLeft + el.clientWidth < el.scrollWidth - 4;
	}
	$effect(() => {
		const el = scroller;
		if (!el) return;
		items;
		sync();
		const ro = new ResizeObserver(sync);
		ro.observe(el);
		return () => ro.disconnect();
	});
	const page = (dir: number) =>
		scroller?.scrollBy({ left: dir * scroller.clientWidth * 0.8, behavior: "smooth" });
	const arrow =
		"grid h-7 w-7 place-items-center rounded-md border border-border text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-default disabled:text-fg-faint disabled:hover:border-border";
</script>

<section class="w-full px-4 pt-6 md:px-6" aria-label={title}>
	<div class="mb-1 flex items-center gap-3">
		<h2 class="text-[15px] font-semibold text-fg">{title}</h2>
		<span class="font-mono text-[11px] text-fg-faint">{items.length}</span>
		<div class="ml-auto flex items-center gap-3">
			{#if canPrev || canNext}
				<div class="hidden gap-1 md:flex">
					<button type="button" class={arrow} disabled={!canPrev} onclick={() => page(-1)} aria-label={i18n.books_scroll_back()}>
						<ChevronLeft size={16} aria-hidden="true" />
					</button>
					<button type="button" class={arrow} disabled={!canNext} onclick={() => page(1)} aria-label={i18n.books_scroll_on()}>
						<ChevronRight size={16} aria-hidden="true" />
					</button>
				</div>
			{/if}
			{#if seeAllHref}
				<a
					href={seeAllHref}
					onclickcapture={(e) => {
						if (!onSeeAll) return;
						e.preventDefault();
						onSeeAll();
					}}
					class="touch-hit inline-flex items-center gap-1 text-[12.5px] font-medium text-accent-text transition hover:text-fg"
				>
					{i18n.books_see_all()}
					<ArrowRight size={14} aria-hidden="true" />
					<span class="sr-only">— {title}</span>
				</a>
			{/if}
		</div>
	</div>
	<div
		bind:this={scroller}
		use:dragScroll
		onscroll={sync}
		class={cn(
			"-mx-4 flex snap-x gap-3 overflow-x-auto scroll-px-4 px-4 pb-2 pt-3 [scrollbar-width:none] md:-mx-6 md:gap-4 md:scroll-px-6 md:px-6 [&::-webkit-scrollbar]:hidden",
		)}
	>
		{#each items as it (`${it.type}-${it.id}`)}
			<div class="w-[112px] shrink-0 snap-start md:w-[140px]">
				<ShelfCard item={it} />
			</div>
		{/each}
	</div>
</section>
