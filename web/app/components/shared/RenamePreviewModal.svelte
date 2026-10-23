<script lang="ts">
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import Modal from "@components/modals/Modal.svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// SeriesRenamePreviewModal with the endpoint passed in, for the media that
	// have no rename modal of their own (artists, books, book series).
	type Plan = { operations: { media_file_id: number; from: string; to: string }[] };
	let { open, path, queryKey, onClose }: { open: boolean; path: string; queryKey: string; onClose: () => void } = $props();

	const preview = createQuery<Plan>(() => ({
		queryKey: [queryKey, path, "rename-preview"],
		queryFn: () => api<Plan>(`${path}/rename?preview=true`, { method: "POST" }),
		enabled: open,
	}));

	const qc = useQueryClient();
	const apply = createMutation(() => ({
		mutationFn: () => api<Plan>(`${path}/rename`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: [queryKey] });
			toast.ok(i18n.rename_done());
			onClose();
		},
		onError: (e: Error) => toast.err(errorText(e, i18n.rename_failed())),
	}));

	let opsCount = $derived(preview.data?.operations.length ?? 0);
</script>

<Modal {open} title={i18n.action_rename_files()} size="lg" {onClose}>
	{#if preview.isLoading}
		<p class="text-sm text-fg-subtle">{i18n.rename_computing()}</p>
	{:else if preview.isError}
		<p class="text-sm text-status-failed">{errorText(preview.error, i18n.rename_plan_failed())}</p>
	{:else if opsCount === 0}
		<p class="text-sm text-fg-muted">{i18n.rename_already_correct()}</p>
	{:else}
		<p class="mb-3 text-sm text-fg-muted">
			{(opsCount === 1 ? i18n.rename_will_move_one : i18n.rename_will_move_other)({ count: opsCount })}
		</p>
		<ul class="flex flex-col gap-2">
			{#each preview.data?.operations ?? [] as op (op.media_file_id)}
				<li class="overflow-hidden rounded-md border border-border bg-bg-card/60 text-xs">
					<div class="flex gap-2.5 px-3 py-2">
						<span class="w-8 shrink-0 pt-px font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint">{i18n.common_from()}</span>
						<span class="min-w-0 break-all font-mono text-fg-muted" title={op.from}>{op.from}</span>
					</div>
					<div class="flex gap-2.5 border-t border-border bg-accent-soft px-3 py-2">
						<span class="w-8 shrink-0 pt-px font-mono text-[9.5px] uppercase tracking-[0.14em] text-accent-text">{i18n.common_to()}</span>
						<span class="min-w-0 break-all font-mono text-fg" title={op.to}>{op.to}</span>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-sm font-medium text-fg hover:border-border-strong"
		>
			{opsCount === 0 ? i18n.common_close() : i18n.common_cancel()}
		</button>
		{#if opsCount > 0}
			<button
				type="button"
				disabled={apply.isPending}
				onclick={() => apply.mutate()}
				class="rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{apply.isPending ? i18n.common_applying() : i18n.common_apply()}
			</button>
		{/if}
	{/snippet}
</Modal>
