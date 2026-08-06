<script lang="ts">
	import type { Snippet } from "svelte";
	import { config } from "../../lib/config.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		formId,
		submitLabel,
		submitDisabled = false,
		onCancel,
		left,
	}: {
		formId: string;
		submitLabel: string;
		submitDisabled?: boolean;
		onCancel: () => void;
		// Rendered first so an `mr-auto` wrapper inside it pushes the
		// cancel/submit pair to the right edge of the footer.
		left?: Snippet;
	} = $props();
</script>

{@render left?.()}
<button
	type="button"
	onclick={onCancel}
	class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
>
	{config.readOnly ? i18n.common_close() : i18n.common_cancel()}
</button>
{#if !config.readOnly}
	<button
		type="submit"
		form={formId}
		disabled={submitDisabled}
		class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
	>
		{submitLabel}
	</button>
{/if}
