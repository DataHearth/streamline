<script lang="ts">
	import type { AnyFormApi } from "@tanstack/form-core";

	type Props = {
		form: AnyFormApi;
		label?: string;
		pendingLabel?: string;
		disabled?: boolean;
		title?: string;
	};

	let {
		form,
		label = "Save",
		pendingLabel = "Saving…",
		disabled = false,
		title,
	}: Props = $props();
</script>

<button
	type="submit"
	{title}
	disabled={disabled || !form.state.canSubmit || form.state.isSubmitting}
	class="inline-flex h-10 w-fit items-center justify-center gap-2 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
>
	{form.state.isSubmitting ? pendingLabel : label}
</button>
