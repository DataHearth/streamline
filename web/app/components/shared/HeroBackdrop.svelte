<script lang="ts">
	import Img from "@components/shared/Img.svelte";
	import { cn } from "@lib/cn";
	import { heroHold } from "@lib/hero-hold.svelte";

	// The blurred art behind a detail hero, object-cover over the section. While
	// text in the hero is open (HeroHold), the layer keeps the height it had and
	// the text runs on over the bg-deep the fade ends at.
	let { src, class: klass = "" }: { src?: string; class?: string } = $props();

	const hold = heroHold();
	let box = $state<HTMLDivElement | null>(null);
	let layer = $state<HTMLDivElement | null>(null);
	let held = $derived(hold?.height ?? null);

	$effect(() => {
		const node = box;
		const art = layer;
		if (!hold || !node || !art) return;
		const measure = () => art.getBoundingClientRect().height;
		hold.measure = measure;
		// A new layout width drops the hold; a pinch-zoom leaves it alone.
		let width: number | undefined;
		const ro = new ResizeObserver(([entry]) => {
			const w = entry?.contentRect.width;
			if (width !== undefined && w !== width) hold.height = null;
			width = w;
		});
		ro.observe(node);
		return () => {
			ro.disconnect();
			if (hold.measure === measure) hold.measure = () => null;
		};
	});
</script>

<div bind:this={box} class="absolute inset-0 z-0 overflow-hidden bg-bg-deep">
	<div
		bind:this={layer}
		data-hero-backdrop
		style:height={held === null ? undefined : `${held}px`}
		class={cn("absolute inset-x-0 top-0 h-full overflow-hidden", held !== null && "max-h-full", klass)}
	>
		{#if src}
			<Img
				{src}
				alt=""
				aria-hidden="true"
				class="h-full w-full scale-110 object-cover opacity-70 blur-md"
			/>
		{/if}
		<div class="hero-overlay absolute inset-0"></div>
	</div>
</div>

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
