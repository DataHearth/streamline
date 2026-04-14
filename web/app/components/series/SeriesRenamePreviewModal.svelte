<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import Modal from "../modals/Modal.svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { SeriesRenamePlan } from "../../lib/types";

	type Props = {
		open: boolean;
		seriesId: number;
		onClose: () => void;
	};
	let { open, seriesId, onClose }: Props = $props();

	const preview = createQuery<SeriesRenamePlan>(() => ({
		queryKey: ["series", seriesId, "rename-preview"],
		queryFn: () =>
			api<SeriesRenamePlan>(`/series/${seriesId}/rename?preview=true`, {
				method: "POST",
			}),
		enabled: open,
	}));

	const qc = useQueryClient();
	const apply = createMutation(() => ({
		mutationFn: () =>
			api<SeriesRenamePlan>(`/series/${seriesId}/rename`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series", seriesId] });
			toast.ok("Files renamed");
			onClose();
		},
		onError: (e: Error) => toast.err(e.message ?? "Rename failed"),
	}));

	let opsCount = $derived(preview.data?.operations.length ?? 0);
</script>

<Modal {open} title="Rename files" size="lg" {onClose}>
	{#if preview.isLoading}
		<p class="text-sm text-fg-subtle">Computing rename plan…</p>
	{:else if preview.isError}
		<p class="text-sm text-status-failed">
			{preview.error?.message ?? "Failed to compute plan"}
		</p>
	{:else if opsCount === 0}
		<p class="text-sm text-fg-muted">Everything is already named correctly.</p>
	{:else}
		<p class="mb-3 text-sm text-fg-muted">
			{opsCount} file{opsCount === 1 ? "" : "s"} will be moved:
		</p>
		<ul class="flex flex-col gap-2">
			{#each preview.data?.operations ?? [] as op (op.media_file_id)}
				<li
					class="grid grid-cols-1 gap-1 rounded-md border border-border bg-bg-card/60 p-3 text-xs"
				>
					<span class="font-mono text-fg-muted" title={op.from}>
						{op.from}
					</span>
					<span class="font-mono text-fg" title={op.to}>
						↓ {op.to}
					</span>
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
			{opsCount === 0 ? "Close" : "Cancel"}
		</button>
		{#if opsCount > 0}
			<button
				type="button"
				disabled={apply.isPending}
				onclick={() => apply.mutate()}
				class="rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-on-accent hover:bg-accent/90 disabled:cursor-not-allowed disabled:opacity-60"
			>
				{apply.isPending ? "Applying…" : "Apply"}
			</button>
		{/if}
	{/snippet}
</Modal>
