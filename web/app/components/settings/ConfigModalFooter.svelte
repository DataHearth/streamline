<script lang="ts">
	import type { Snippet } from "svelte";
	import { cn } from "@lib/cn";
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
<button
	type="button"
	onclick={onCancel}
	class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
>
	{submitShown ? i18n.common_cancel() : i18n.common_close()}
</button>
{#if submitShown}
	<button
		type="submit"
		form={formId}
		disabled={submitDisabled}
		class={cn(
			"inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md px-4 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-60",
			submitTone === "danger"
				? "bg-status-failed text-bg-deep hover:bg-status-failed/90"
				: "bg-accent text-fg-on-accent hover:bg-accent-hover",
		)}
	>
		{submitLabel}
	</button>
{/if}
