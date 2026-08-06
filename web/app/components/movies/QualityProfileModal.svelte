<script lang="ts">
	import { untrack } from "svelte";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import type { QualityProfile } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		current?: string;
		profiles: QualityProfile[];
		saving?: boolean;
		onClose: () => void;
		onSave: (profile: string) => void;
	};
	let { open, current, profiles, saving = false, onClose, onSave }: Props =
		$props();

	// One-time initialisation: `selected` is then driven by bind:value.
	// "" is a real choice (server default), not a missing value.
	let selected = $state<string>(untrack(() => current ?? ""));
</script>

<Modal {open} title={i18n.action_change_quality_profile()} size="md" {onClose}>
	<Select
		label={i18n.quality_profile()}
		value={selected}
		options={[
			{ value: "", label: i18n.quality_server_default() },
			...profiles.map((p) => ({ value: p.name, label: p.name })),
		]}
		onChange={(v) => (selected = v)}
	/>
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
			disabled={saving || selected === (current ?? "")}
			onclick={() => onSave(selected)}
			class="rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{saving ? i18n.common_saving() : i18n.common_save()}
		</button>
	{/snippet}
</Modal>
