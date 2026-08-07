<script lang="ts">
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Lock } from "@lucide/svelte";
	import { config } from "../../lib/config.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Read-only is a property of the instance, not news about this page. The
	// two-line banner it replaces sat above all twelve settings pages and said
	// the same sentence every time — on a phone that is the first screenful you
	// meet on arrival, every arrival. This states it in one line and keeps the
	// explanation one tap away.
	//
	// The locks on the individual controls are what actually answer "can I
	// change this?" — see readOnlyLock() and FieldLock.
	let expanded = $state(false);
</script>

{#if config.readOnly}
	<div
		class="border-b border-status-wanted/25 bg-status-wanted/[0.08] text-status-wanted"
	>
		<div class="mx-auto flex w-full max-w-7xl items-center gap-2.5 px-4 md:px-8">
			<Lock size={13} class="shrink-0" aria-hidden="true" />
			<p class="py-2 text-xs font-medium">
				{i18n.settings_readonly_strip()}
			</p>
			<button
				type="button"
				aria-expanded={expanded}
				onclick={() => (expanded = !expanded)}
				class="ml-auto shrink-0 rounded px-1.5 py-2 text-[11px] text-status-wanted/75 underline-offset-2 transition hover:text-status-wanted hover:underline"
			>
				{expanded ? i18n.common_hide() : i18n.common_why()}
			</button>
		</div>
		{#if expanded}
			<div transition:slide={{ duration: 180, easing: cubicOut }}>
				<p
					class="mx-auto w-full max-w-7xl px-4 pb-3 text-[11.5px] leading-relaxed text-status-wanted/80 md:px-8"
				>
					{i18n.settings_readonly_banner()}
				</p>
			</div>
		{/if}
	</div>
{/if}
