<script lang="ts">
	import { Check, Copy, TriangleAlert } from "@lucide/svelte";
	import { toast } from "@lib/toast";
	import Modal from "@components/modals/Modal.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		title: string;
		yaml: string | null;
		onClose: () => void;
	};
	let { title, yaml, onClose }: Props = $props();

	let copied = $state(false);

	async function copy() {
		if (!yaml) return;
		try {
			await navigator.clipboard.writeText(yaml);
			copied = true;
			toast.ok(i18n.common_copied());
			setTimeout(() => (copied = false), 1500);
		} catch {
			toast.err(i18n.common_clipboard_unavailable());
		}
	}
</script>

<Modal open={yaml !== null} {title} size="lg" {onClose}>
	<div class="space-y-4 text-sm text-fg">
		<div
			class="flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
		>
			<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<p>{i18n.cardigann_yaml_help()}</p>
		</div>
		<div>
			<div class="mb-1 flex items-center justify-between">
				<span class="text-[11px] font-medium text-fg-muted">config.yaml</span>
				<button
					type="button"
					onclick={copy}
					class="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
				>
					{#if copied}
						<Check size={12} class="text-status-available" aria-hidden="true" />
						{i18n.common_copied()}
					{:else}
						<Copy size={12} aria-hidden="true" />
						{i18n.cardigann_copy_yaml()}
					{/if}
				</button>
			</div>
			<pre
				class="overflow-x-auto rounded-md border border-border bg-bg-base p-3 font-mono text-[11px] leading-relaxed text-fg">{yaml}</pre>
		</div>
	</div>

	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			{i18n.common_close()}
		</button>
	{/snippet}
</Modal>
