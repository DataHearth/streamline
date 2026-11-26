<script lang="ts">
	import Img from "@components/shared/Img.svelte";
	import { auth } from "@lib/auth.svelte";

	// A detail hero's title: the title's logo from lg up, the text below it and
	// wherever there is no logo. Under lg the poster column is too short for a
	// logo's height, which would push the title block above it. The CSS picks,
	// and a lazy image that is not displayed is never fetched, so a phone or a
	// tablet downloads nothing. An account with "Title logos" off always
	// gets the text.
	let { title, logoSrc }: { title: string; logoSrc?: string } = $props();

	// A logo that fails to load falls back to the text; another title on the
	// same component, or the same one under a new match, gets its own try.
	let failedSrc = $state<string | null>(null);
	let shown = $derived(
		auth.user?.title_logos !== false && logoSrc && logoSrc !== failedSrc ? logoSrc : null,
	);
</script>

{#if shown}
	<span class="lg:hidden">{title}</span>
	<!-- Keyed: an <img> keeps its picture until the next one loads, which
	     would leave the previous title's logo over this one. -->
	{#key shown}
		<Img
			src={shown}
			alt={title}
			loading="lazy"
			decoding="async"
			class="hidden h-auto w-auto object-contain object-left drop-shadow-[0_2px_12px_rgb(0_0_0_/0.55)] lg:block lg:max-h-48 lg:max-w-[min(36rem,100%)]"
			onerror={() => (failedSrc = shown)}
		/>
	{/key}
{:else}
	{title}
{/if}
