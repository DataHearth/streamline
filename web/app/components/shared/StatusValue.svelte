<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import { DOWNLOAD_CHIP, DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A status written in a details table: the word alone, or — while a
	// download is behind it — the word as a link to that download on Activity,
	// in the same chip a history row's "Download ›" wears.
	let { label, href }: { label: string; href?: string } = $props();
</script>

{#if href}
	<a {href} title={i18n.detail_open_download()} class={DOWNLOAD_CHIP}>
		{label}
		<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
	</a>
{:else}
	{label}
{/if}
