<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import { DOWNLOAD_CHIP, DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A status written as a word in a details table: the word alone, or, while
	// a download is behind it, the word as the link to that download on
	// Activity, in the chip the events feed's "Download" wears. The negative
	// margin takes back touch-hit's padding, so the table's row does not grow.
	let { label, href }: { label: string; href?: string } = $props();
</script>

{#if href}
	<a {href} title={i18n.activity_download_open()} class="group/dl touch-hit -my-[15px] inline-flex cursor-pointer lg:my-0">
		<span class={DOWNLOAD_CHIP}>
			{label}
			<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
		</span>
		<span class="sr-only">{i18n.activity_download_open()}</span>
	</a>
{:else}
	{label}
{/if}
