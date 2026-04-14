<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Key, Trash2 } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import type { ApiKey } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";

	let {
		userId,
		apiKeys,
	}: {
		userId: number;
		apiKeys: ApiKey[];
	} = $props();

	let revoking = $state<ApiKey | null>(null);

	const qc = useQueryClient();

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/users/${userId}/api-keys/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", userId] });
			toast.ok("Key revoked");
		},
		onError: (err) => toast.err(err.message),
	}));
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header
		class="flex items-center justify-between border-b border-border px-5 py-3.5"
	>
		<div>
			<h3 class="text-base font-semibold text-fg">API keys</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				{apiKeys.length}
				{apiKeys.length === 1 ? "key" : "keys"} on record
			</p>
		</div>
	</header>

	{#if apiKeys.length === 0}
		<div class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted">
			<Key size={16} aria-hidden="true" />
			<span>No API keys on record.</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each apiKeys as k (k.id)}
				<li class="flex items-start gap-3.5 px-5 py-3.5">
					<div
						class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted"
						aria-hidden="true"
					>
						<Key size={18} />
					</div>
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-semibold text-fg">{k.name}</p>
						<dl
							class="mt-1 flex flex-wrap gap-x-3.5 gap-y-0.5 text-xs text-fg-muted"
						>
							<div class="flex items-center gap-1">
								<dt class="text-fg-subtle">created</dt>
								<dd title={formatDateTime(k.created_at)}
									>{formatRelative(k.created_at)}</dd
								>
							</div>
							<div class="flex items-center gap-1">
								<dt class="text-fg-subtle">last used</dt>
								<dd
									class:text-fg-subtle={!k.last_used_at}
									title={k.last_used_at
										? formatDateTime(k.last_used_at)
										: undefined}
								>
									{k.last_used_at
										? formatRelative(k.last_used_at)
										: "never"}
								</dd>
							</div>
						</dl>
					</div>
					<button
						type="button"
						disabled={revoke.isPending}
						onclick={() => (revoking = k)}
						class="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:opacity-40"
						aria-label={`Revoke API key ${k.name}`}
					>
						<Trash2 size={14} aria-hidden="true" />
						Revoke
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<Dialog
	open={revoking !== null}
	title="Revoke '{revoking?.name ?? ''}'?"
	body="Anything using this key will immediately lose access."
	onClose={() => (revoking = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Revoke",
			variant: "danger",
			onClick: () => revoking && revoke.mutate(revoking.id),
		},
	]}
/>
