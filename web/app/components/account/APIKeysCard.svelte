<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Key, Plus, Trash2, Clipboard, X, ShieldAlert } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import type { ApiKey } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";

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
		onError: (err) => toast.err(err.message),
	}));

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/auth/me/api-keys/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["auth", "me", "api-keys"] });
			toast.ok("Key revoked");
		},
		onError: (err) => toast.err(err.message),
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
			<h3 class="text-base font-semibold text-fg">API keys</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				For mobile apps and CLI tooling. Tokens are shown once.
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
					>New key name</span
				>
				<input
					bind:value={newName}
					placeholder="e.g. iOS, CLI"
					class="h-9 w-full rounded-md border border-border bg-bg px-3 text-sm text-fg placeholder:text-fg-faint focus-visible:outline-2 focus-visible:outline-accent"
				/>
			</label>
			<button
				type="submit"
				disabled={create.isPending || newName.trim().length === 0}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Plus size={14} aria-hidden="true" />
				{create.isPending ? "Creating…" : "Create"}
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
						Copy this token now
					</p>
					<p class="mt-0.5 text-xs text-fg-muted">
						It won't be shown again. Treat it like a password.
					</p>
				</div>
				<button
					type="button"
					onclick={() => (revealed = null)}
					class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					aria-label="Dismiss"
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
				Copy to clipboard
			</button>
		</div>
	{/if}

	{#if keys.isPending}
		<p class="px-5 py-6 text-sm text-fg-subtle">Loading…</p>
	{:else if keys.isError}
		<p class="px-5 py-6 text-sm text-status-failed">
			Failed to load: {keys.error?.message}
		</p>
	{:else if items.length === 0}
		<div class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted">
			<Key size={16} aria-hidden="true" />
			<span>No keys yet.</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each items as k (k.id)}
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
						onclick={() => (revoking = k)}
						class="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed transition hover:bg-status-failed/10"
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
