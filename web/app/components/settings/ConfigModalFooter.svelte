<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import type { Snippet } from "svelte";
	import { config } from "@lib/config.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		formId,
		submitLabel,
		submitDisabled = false,
		showSubmit: showSubmitProp,
		submitTone = "accent",
		onCancel,
		left,
	}: {
		formId: string;
		submitLabel: string;
		submitDisabled?: boolean;
		// A form that writes nothing back (one that only shows its result)
		// keeps its submit on a read-only instance.
		showSubmit?: boolean;
		// "danger" for a submit that grants something broad: the red button
		// the confirm dialogs use.
		submitTone?: "accent" | "danger";
		onCancel: () => void;
		// Rendered first so an `mr-auto` wrapper inside it pushes the
		// cancel/submit pair to the right edge of the footer.
		left?: Snippet;
	} = $props();
	let submitShown = $derived(showSubmitProp ?? !config.readOnly);
</script>

{@render left?.()}
<Button variant="secondary" type="button" onclick={onCancel}>
	{submitShown ? i18n.common_cancel() : i18n.common_close()}
</Button>
{#if submitShown}
	<Button
		variant={submitTone === "danger" ? "danger" : "primary"}
		type="submit"
		form={formId}
		disabled={submitDisabled}
	>
		{submitLabel}
	</Button>
{/if}
