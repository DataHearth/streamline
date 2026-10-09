<script lang="ts">
	import { BookOpen } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Poster from "@components/shared/Poster.svelte";

	let {
		src,
		alt = "",
		dim = false,
		dashed = false,
		class: klass = "rounded-lg",
	}: { src?: string; alt?: string; dim?: boolean; dashed?: boolean; class?: string } = $props();
</script>

<div class={cn("relative aspect-[2/3] w-full overflow-hidden", klass)}>
	<div class="absolute inset-0 grid place-items-center bg-bg-card text-fg-faint">
		<BookOpen class="h-1/5 w-1/5" aria-hidden="true" />
	</div>
	{#if src}
		<Poster {src} {alt} class={cn("relative h-full w-full object-cover", dim && "opacity-35 grayscale")} />
	{/if}
	{#if dashed}
		<div class={cn("pointer-events-none absolute inset-0 border border-dashed border-white/40", klass)}></div>
	{/if}
</div>
