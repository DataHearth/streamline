<script lang="ts">
	import type { Snippet } from "svelte";
	import { ChevronRight } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a page already shows of a download — its status pill — as the link
	// to the record's detail on Activity: a pointer cursor, and a "›" after it
	// on hover and keyboard focus (always on a touch screen), the way every
	// status that links a download reads. Without a download it is the bare
	// content. The torrent is one step further, from that detail.
	let {
		href,
		class: klass = "",
		children,
	}: {
		href: string | undefined;
		class?: string;
		children: Snippet;
	} = $props();
</script>

{#if href}
	<a
		{href}
		title={i18n.detail_open_download()}
		class={cn(
			"group/dl inline-flex cursor-pointer items-center gap-1 rounded-full transition hover:brightness-125 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
			klass,
		)}
	>
		{@render children()}
		<span class={`${DOWNLOAD_CHIP_ARROW} text-accent-text`}><ChevronRight size={12} aria-hidden="true" /></span>
		<span class="sr-only">{i18n.detail_open_download()}</span>
	</a>
{:else}
	{@render children()}
{/if}
