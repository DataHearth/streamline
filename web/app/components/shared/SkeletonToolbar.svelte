<script lang="ts">
	let { triggers = 2 }: { triggers?: number } = $props();

	const bar =
		"animate-pulse rounded-md bg-white/[0.06] motion-reduce:animate-none";
	// Keyed trigger widths in source order: status, type, monitoring. Close
	// enough to the real labels that the row wraps onto the same number of
	// lines at the same widths.
	const W = [152, 132, 150];
</script>

<!--
  Stands in for MoviesToolbar / SeriesToolbar during the initial load, where
  the real toolbar does not exist yet: without it the grid starts high and the
  whole page drops when data lands, which is the one thing a skeleton is for.

  It mirrors the real toolbars' STRUCTURE rather than a measured height — same
  wrapper classes, same control heights, same flex-wrap — so it breaks onto a
  second line at the same width the real one does. `triggers` is the only
  difference between the two pages: movies has status and monitoring, series
  adds type.
-->
<div class="sticky top-16 z-20 bg-bg-deep/85 backdrop-blur-md md:hidden">
	<div class="flex items-center gap-2 px-4 py-2">
		<div class="flex flex-1 items-center gap-2 overflow-hidden">
			{#each [86, 74, 96] as w}
				<div class="{bar} h-11 shrink-0 rounded-full" style="width:{w}px"></div>
			{/each}
		</div>
		<div class="{bar} h-11 w-11 shrink-0 rounded-lg"></div>
	</div>
</div>

<div
	class="sticky top-16 z-20 hidden flex-wrap items-center gap-2 bg-bg-deep/85 px-4 py-3 backdrop-blur-md md:flex md:gap-2.5 md:px-6"
	aria-hidden="true"
>
	{#each Array(triggers) as _, i}
		<div class="{bar} order-1 h-11 lg:h-9" style="width:{W[i]}px"></div>
	{/each}
	<div class="{bar} order-2 h-11 min-w-[7rem] flex-1 lg:h-9"></div>
	<div class="order-3 ml-auto flex items-center gap-2">
		<div class="{bar} hidden h-11 w-[170px] md:block lg:h-9"></div>
		<div class="{bar} h-11 w-[72px] lg:h-8 lg:w-[62px]"></div>
		<div class="{bar} h-11 w-11 lg:h-9 lg:w-9"></div>
		<div class="{bar} hidden h-11 w-[124px] lg:block lg:h-9"></div>
	</div>
</div>
