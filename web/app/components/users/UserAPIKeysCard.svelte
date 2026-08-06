<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Key } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { ApiKey } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";
	import ApiKeyRow from "../shared/ApiKeyRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header
		class="flex items-center justify-between border-b border-border px-5 py-3.5"
	>
		<div>
			<h3 class="text-base font-semibold text-fg">{i18n.account_api_keys()}</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				{apiKeys.length}
				{apiKeys.length === 1 ? "key" : "keys"} on record
			</p>
		</div>
	</header>

	{#if apiKeys.length === 0}
		<div class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted">
			<Key size={16} aria-hidden="true" />
			<span>{i18n.account_no_keys_record()}</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each apiKeys as k (k.id)}
				<ApiKeyRow
					apiKey={k}
					revoking={revoke.isPending}
					onRevoke={() => (revoking = k)}
				/>
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
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_revoke(),
			variant: "danger",
			onClick: () => revoking && revoke.mutate(revoking.id),
		},
	]}
/>
