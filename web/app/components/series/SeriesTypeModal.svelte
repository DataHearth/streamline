<script lang="ts">
	import { untrack } from "svelte";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import type { SeriesType } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		current?: SeriesType;
		saving?: boolean;
		onClose: () => void;
		onSave: (type: SeriesType) => void;
	};
	let { open, current, saving = false, onClose, onSave }: Props = $props();

	const options: { value: SeriesType; label: string }[] = [
		{ value: "standard", label: i18n.lc_standard() },
		{ value: "anime", label: i18n.lc_anime() },
		{ value: "daily", label: i18n.lc_daily() },
	];

	let selected = $state<SeriesType>(untrack(() => current ?? "standard"));
</script>

<Modal {open} title={i18n.series_type()} size="md" {onClose}>
	<Select
		label={i18n.series_type()}
		value={selected}
		{options}
		onChange={(v) => (selected = v as SeriesType)}
	/>
	<p class="mt-3 text-xs leading-relaxed text-fg-muted">
		{i18n.series_type_help()}
	</p>
	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-sm font-medium text-fg hover:border-border-strong"
		>
			{i18n.common_cancel()}
		</button>
		<button
			type="button"
			disabled={saving || selected === current}
			onclick={() => onSave(selected)}
			class="rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{saving ? i18n.common_saving() : i18n.common_save()}
		</button>
	{/snippet}
</Modal>
