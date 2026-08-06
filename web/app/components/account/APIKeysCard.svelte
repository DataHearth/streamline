<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Key, Plus, Clipboard, X, ShieldAlert } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { ApiKey } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";
	import ApiKeyRow from "../shared/ApiKeyRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type ApiKeyCreated = ApiKey & { raw_token: string };

	const qc = useQueryClient();

	let revoking = $state<ApiKey | null>(null);

	const keys = createQuery<ApiKey[]>(() => ({
		queryKey: ["auth", "me", "api-keys"],
		queryFn: () => api<ApiKey[]>("/auth/me/api-keys"),
	}));

	let newName = $state("");
	let revealed = $state<ApiKeyCreated | null>(null);

	const create = createMutation<ApiKeyCreated, Error, string>(() => ({
		mutationFn: (name) =>
			api<ApiKeyCreated>("/auth/me/api-keys", {
				method: "POST",
				body: { name },
			}),
		onSuccess: (resp) => {
			revealed = resp;
			newName = "";
			qc.invalidateQueries({ queryKey: ["auth", "me", "api-keys"] });
			toast.ok("API key created");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/auth/me/api-keys/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["auth", "me", "api-keys"] });
			toast.ok("Key revoked");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	async function copyRaw() {
		if (!revealed) return;
		try {
			await navigator.clipboard.writeText(revealed.raw_token);
			toast.ok("Copied");
		} catch {
			toast.err("Clipboard unavailable");
		}
	}

	let items = $derived(keys.data ?? []);
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header
		class="flex items-center justify-between border-b border-border px-5 py-3.5"
	>
		<div>
			<h3 class="text-base font-semibold text-fg">{i18n.account_api_keys()}</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				{i18n.account_api_keys_help()}
			</p>
		</div>
	</header>

	<div class="border-b border-border px-5 py-4">
		<form
			class="flex flex-wrap items-end gap-2"
			onsubmit={(e) => {
				e.preventDefault();
				if (newName.trim().length === 0) return;
				create.mutate(newName.trim());
			}}
		>
			<label class="min-w-[200px] flex-1">
				<span class="mb-1 block text-xs font-medium text-fg-muted"
					>{i18n.account_new_key_name()}</span
				>
				<input
					bind:value={newName}
					placeholder={i18n.account_key_name_example()}
					class="h-9 w-full rounded-md border border-border bg-bg px-3 text-sm text-fg placeholder:text-fg-faint focus-visible:outline-2 focus-visible:outline-accent"
				/>
			</label>
			<button
				type="submit"
				disabled={create.isPending || newName.trim().length === 0}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Plus size={14} aria-hidden="true" />
				{create.isPending ? i18n.common_creating() : i18n.common_create()}
			</button>
		</form>
	</div>

	{#if revealed}
		<div
			class="flex flex-col gap-2 border-b border-status-wanted/30 bg-status-wanted/5 px-5 py-4"
		>
			<div class="flex items-start gap-2">
				<ShieldAlert
					size={16}
					class="mt-0.5 shrink-0 text-status-wanted"
					aria-hidden="true"
				/>
				<div class="min-w-0 flex-1">
					<p class="text-sm font-semibold text-fg">
						{i18n.account_copy_token_now()}
					</p>
					<p class="mt-0.5 text-xs text-fg-muted">
						{i18n.account_token_once()}
					</p>
				</div>
				<button
					type="button"
					onclick={() => (revealed = null)}
					class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					aria-label={i18n.common_dismiss()}
				>
					<X size={14} aria-hidden="true" />
				</button>
			</div>
			<code
				class="block break-all rounded-md bg-bg-deep px-3 py-2 font-mono text-xs text-fg"
				>{revealed.raw_token}</code
			>
			<button
				type="button"
				onclick={copyRaw}
				class="inline-flex h-8 w-fit items-center gap-1.5 rounded-md border border-border bg-bg-base px-2.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
			>
				<Clipboard size={12} aria-hidden="true" />
				{i18n.common_copy_clipboard()}
			</button>
		</div>
	{/if}

	{#if keys.isPending}
		<p class="px-5 py-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if keys.isError}
		<p class="px-5 py-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(keys.error) })}
		</p>
	{:else if items.length === 0}
		<div class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted">
			<Key size={16} aria-hidden="true" />
			<span>{i18n.account_no_keys()}</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each items as k (k.id)}
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
