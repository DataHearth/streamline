<script lang="ts">
	import { untrack } from "svelte";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import type { QualityProfile } from "../../lib/types";

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
	let selected = $state<string>(
		untrack(() => current ?? profiles[0]?.name ?? ""),
	);
</script>

<Modal {open} title="Change quality profile" size="md" {onClose}>
	<Select
		label="Quality profile"
		value={selected}
		options={profiles.map((p) => ({ value: p.name, label: p.name }))}
		onChange={(v) => (selected = v)}
	/>
	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-sm font-medium text-fg hover:border-border-strong"
		>
			Cancel
		</button>
		<button
			type="button"
			disabled={saving || selected === current}
			onclick={() => onSave(selected)}
			class="rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-on-accent hover:bg-accent/90 disabled:cursor-not-allowed disabled:opacity-60"
		>
			{saving ? "Saving…" : "Save"}
		</button>
	{/snippet}
</Modal>
