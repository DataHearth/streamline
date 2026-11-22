<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import type { AnyFormApi } from "@tanstack/form-core";
	import { readOnlyLock } from "@lib/config.svelte";

	type Props = {
		form: AnyFormApi;
		label?: string;
		pendingLabel?: string;
		disabled?: boolean;
		title?: string;
	};

	let {
		form,
		label = i18n.common_save(),
		pendingLabel = i18n.common_saving(),
		disabled = false,
		title,
	}: Props = $props();

	const lock = readOnlyLock();
</script>

<Button
	class="w-fit"
	type="submit"
	{title}
	disabled={disabled ||
		lock() ||
		!form.state.canSubmit ||
		form.state.isSubmitting}
>
	{form.state.isSubmitting ? pendingLabel : label}
</Button>
