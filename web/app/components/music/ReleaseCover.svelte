<script lang="ts">
	import { Music } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Poster from "@components/shared/Poster.svelte";

	// Square art for a release. Greyed when nothing of it is in the library,
	// dashed when it is not out yet — the same two marks everywhere a release
	// appears.
	let {
		src,
		alt = "",
		dim = false,
		dashed = false,
		class: klass = "rounded-md",
	}: { src: string; alt?: string; dim?: boolean; dashed?: boolean; class?: string } = $props();
</script>

<div class={cn("relative aspect-square w-full overflow-hidden", klass)}>
	<div class="absolute inset-0 grid place-items-center bg-bg-card text-fg-faint">
		<Music class="h-1/4 w-1/4" aria-hidden="true" />
	</div>
	<Poster {src} {alt} class={cn("relative h-full w-full object-cover", dim && "opacity-35 grayscale")} />
	{#if dashed}
		<div class={cn("pointer-events-none absolute inset-0 border border-dashed border-white/40", klass)}></div>
	{/if}
</div>
