<script lang="ts">
	import { Music } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import Poster from "@components/shared/Poster.svelte";
	import ChangeMark from "@components/changes/ChangeMark.svelte";
	import type { CoverMark } from "@lib/metadata-changes";

	// Square art for a release. Greyed when nothing of it is in the library,
	// dashed when it is not out yet — the same two marks everywhere a release
	// appears.
	let {
		src,
		alt = "",
		dim = false,
		dashed = false,
		class: klass = "rounded-md",
		mark,
		compact = false,
	}: {
		src: string;
		alt?: string;
		dim?: boolean;
		dashed?: boolean;
		class?: string;
		// A provider change waits on this album (lib/metadata-changes).
		mark?: CoverMark;
		compact?: boolean;
	} = $props();
</script>

<div class={twMerge("relative aspect-square w-full overflow-hidden", klass)}>
	<div class="absolute inset-0 grid place-items-center bg-bg-card text-fg-faint">
		<Music class="h-1/4 w-1/4" aria-hidden="true" />
	</div>
	<Poster {src} {alt} class={twMerge("relative h-full w-full object-cover", dim && "opacity-35 grayscale")} />
	<ChangeMark {mark} {compact} />
	{#if dashed}
		<div class={twMerge("pointer-events-none absolute inset-0 border border-dashed border-white/40", klass)}></div>
	{/if}
</div>
