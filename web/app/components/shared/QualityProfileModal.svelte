<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import { untrack } from "svelte";
	import Modal from "@components/modals/Modal.svelte";
	import Select from "@components/forms/Select.svelte";
	import type { QualityProfile } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

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
		<Button
			variant="secondary"
			size="sm"
			type="button"
			onclick={onClose}
		>
			{i18n.common_cancel()}
		</Button>
		<Button
			size="sm"
			type="button"
			disabled={saving || selected === (current ?? "")}
			onclick={() => onSave(selected)}
		
		>
			{saving ? i18n.common_saving() : i18n.common_save()}
		</Button>
	{/snippet}
</Modal>
