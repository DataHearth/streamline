<script lang="ts">
	// Holds the page's shape while the first answer is on its way: a row of
	// figures, a chart, two panels. Later range changes keep the last answer on
	// screen instead (routes/stats), so this only shows once.
	const bar = "rounded bg-bg-hover motion-safe:animate-pulse";
</script>

<div class="flex flex-col gap-4" aria-hidden="true">
	<div class="grid grid-cols-2 gap-4 md:grid-cols-4">
		{#each Array.from({ length: 4 }) as _, i (i)}
			<div class="rounded-lg border border-border bg-bg-elevated p-4">
				<div class="{bar} h-2.5 w-20"></div>
				<div class="{bar} mt-3 h-6 w-24"></div>
			</div>
		{/each}
	</div>
	<div class="rounded-lg border border-border bg-bg-elevated p-5">
		<div class="{bar} h-3 w-32"></div>
		<div class="{bar} mt-5 h-32 w-full"></div>
	</div>
	<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
		{#each Array.from({ length: 2 }) as _, i (i)}
			<div class="rounded-lg border border-border bg-bg-elevated p-5">
				<div class="{bar} h-3 w-28"></div>
				<div class="{bar} mt-4 h-2 w-full"></div>
				<div class="{bar} mt-3 h-2 w-4/5"></div>
				<div class="{bar} mt-3 h-2 w-3/5"></div>
			</div>
		{/each}
	</div>
</div>
