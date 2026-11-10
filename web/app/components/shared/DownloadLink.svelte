<script lang="ts">
	import type { Snippet } from "svelte";
	import { ChevronRight } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a page already shows of a download, its status, as the link to the
	// record on Activity: a pointer cursor, and the events feed's "›" after it
	// on hover and keyboard focus (always on a touch screen). Without a
	// download it is the bare content, and nothing says "link". Below lg the
	// negative margin takes back touch-hit's padding: the tap target grows over
	// the row the status sits in, and the row does not move.
	let {
		href,
		label = i18n.activity_download_open(),
		class: klass = "",
		children,
	}: {
		href: string | undefined;
		// What the link opens, for its tooltip and for a screen reader.
		label?: string;
		class?: string;
		children: Snippet;
	} = $props();
</script>

{#if href}
	<a
		{href}
		title={label}
		class={cn(
			"group/dl touch-hit -my-[15px] inline-flex cursor-pointer items-center gap-1 rounded-full transition hover:brightness-125 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent lg:my-0",
			klass,
		)}
	>
		{@render children()}
		<span class={cn(DOWNLOAD_CHIP_ARROW, "text-accent-text")}
			><ChevronRight size={12} aria-hidden="true" /></span
		>
		<span class="sr-only">{label}</span>
	</a>
{:else}
	{@render children()}
{/if}
