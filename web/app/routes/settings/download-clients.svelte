<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Download, Pencil, Eye, Lock } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { downloadClientForm } from "../../lib/schemas";
	import type {
		DownloadClient,
		DownloadClientType,
		DownloadClientAuth,
	} from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import DownloadClientForm from "../../components/settings/forms/DownloadClientForm.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";

	type Values = {
		name: string;
		client_type: DownloadClientType;
		host: string;
		port: number;
		auth_method: DownloadClientAuth;
		username: string;
		password: string;
		api_key: string;
		use_ssl: boolean;
		priority: number;
		enabled: boolean;
	};

	const qc = useQueryClient();

	const list = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
	}));

	let editing = $state<DownloadClient | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<DownloadClient, Error, Values>(() => ({
		mutationFn: (body) => {
			// Drop blank password / api_key on edit so the backend keeps the existing secret.
			const payload: Record<string, unknown> = { ...body };
			if (editing && payload.password === "") delete payload.password;
			if (editing && payload.api_key === "") delete payload.api_key;
			if (editing) {
				return api<DownloadClient>(
					`/download-clients/${encodeURIComponent(editing.name)}`,
					{
						method: "PUT",
						body: payload,
					},
				);
			}
			return api<DownloadClient>("/download-clients", {
				method: "POST",
				body: payload,
			});
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["download-clients"] });
			toast.ok(editing ? "Client updated" : "Client added");
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(err.message),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/download-clients/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["download-clients"] });
			toast.ok("Client deleted");
		},
		onError: (err) => toast.err(err.message),
	}));

	const defaults: Values = {
		name: "",
		client_type: "qbittorrent",
		host: "",
		port: 8080,
		auth_method: "password",
		username: "",
		password: "",
		api_key: "",
		use_ssl: false,
		priority: 25,
		enabled: true,
	};

	const form = createForm(() => ({
		defaultValues: defaults,
		validators: { onChange: downloadClientForm },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openCreate() {
		editing = null;
		form.reset(defaults);
		modalOpen = true;
	}

	function openEdit(c: DownloadClient) {
		editing = c;
		form.reset({
			name: c.name,
			client_type: c.client_type,
			host: c.host,
			port: c.port,
			auth_method: c.auth_method,
			username: c.username ?? "",
			password: "",
			api_key: "",
			use_ssl: c.use_ssl ?? false,
			priority: c.priority ?? 25,
			enabled: c.enabled,
		});
		modalOpen = true;
	}

	let deleting = $state<DownloadClient | null>(null);
	function onDelete(c: DownloadClient) {
		deleting = c;
	}

	let items = $derived(list.data ?? []);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				Download clients
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				Torrent clients Streamline pushes grabs to.
			</p>
		</div>
		<button
			type="button"
			onclick={openCreate}
			disabled={config.readOnly}
			title={config.readOnly ? READONLY_HINT : null}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			<Plus size={16} aria-hidden="true" />
			Add client
		</button>
	</header>

	<div class="mt-6 grid gap-3 sm:grid-cols-2">
		{#if list.isPending}
			<p class="text-sm text-fg-subtle">Loading…</p>
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				Failed to load: {list.error?.message}
			</p>
		{:else if items.length === 0}
			<div
				class="col-span-full rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
			>
				<Download
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">No download clients yet.</p>
				<p class="mt-1 text-xs text-fg-muted">
					Add a qBittorrent, Transmission, or Deluge instance to receive
					grabs.
				</p>
			</div>
		{:else}
			{#each items as c (c.name)}
				<div
					class="group relative flex flex-col gap-3 overflow-hidden rounded-lg border border-border bg-bg-elevated p-5 transition hover:border-border-strong"
				>
					<button
						type="button"
						onclick={() => openEdit(c)}
						class="flex items-start gap-3 text-left"
						aria-label="{config.readOnly ? 'View' : 'Edit'} {c.name}"
					>
						<div
							class="flex h-12 w-12 shrink-0 items-center justify-center rounded-md bg-bg-card"
						>
							<BrandLogo name={c.client_type} size={24} />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate text-base font-semibold text-fg">
									{c.name}
								</span>
								{#if c.enabled}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										enabled
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
									>
										disabled
									</span>
								{/if}
								{#if (c.auth_method === "api_key" ? c.api_key_set : c.password_set)}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										credentials set
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
									>
										credentials missing
									</span>
								{/if}
							</div>
							<div
								class="mt-0.5 truncate font-mono text-xs text-fg-muted"
							>
								{c.use_ssl ? "https" : "http"}://{c.host}:{c.port}
							</div>
							<div class="mt-0.5 text-[11px] text-fg-subtle">
								{c.client_type}
							</div>
						</div>
					</button>
					<div
						class="mt-1 flex items-center justify-between gap-1 border-t border-border pt-3"
					>
						<TestConnectionButton
							endpoint="/download-clients/{encodeURIComponent(c.name)}/test"
						/>
						<div class="flex items-center gap-1">
							{#if config.readOnly}
								<button
									type="button"
									onclick={() => openEdit(c)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="View client"
								>
									<Eye size={16} aria-hidden="true" />
								</button>
							{:else}
								<button
									type="button"
									onclick={() => openEdit(c)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="Edit client"
								>
									<Pencil size={16} aria-hidden="true" />
								</button>
								<button
									type="button"
									onclick={() => onDelete(c)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
									aria-label="Delete client"
								>
									<Trash2 size={16} aria-hidden="true" />
								</button>
							{/if}
						</div>
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>

<Modal
	open={modalOpen}
	title={config.readOnly
		? "View download client"
		: editing
			? "Edit download client"
			: "Add download client"}
	size="xl"
	onClose={() => (modalOpen = false)}
>
	<form
		id="download-client-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		{#if config.readOnly}
			<div
				class="mb-4 flex items-center gap-2 rounded-md border border-border bg-bg-card px-3 py-2 text-xs text-fg-muted"
			>
				<Lock size={14} aria-hidden="true" />
				<span>{READONLY_HINT}</span>
			</div>
		{/if}
		<fieldset disabled={config.readOnly} class="min-w-0">
			<DownloadClientForm {form} isEdit={editing !== null} />
		</fieldset>
	</form>

	{#snippet footer()}
		<div class="mr-auto">
			{#if editing}
				<TestConnectionButton
					endpoint="/download-clients/{encodeURIComponent(editing.name)}/test"
					size="md"
				/>
			{:else}
				<TestConnectionButton
					endpoint="/download-clients/test"
					body={() => form.state.values}
					size="md"
				/>
			{/if}
		</div>
		<button
			type="button"
			onclick={() => (modalOpen = false)}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
		>
			{config.readOnly ? "Close" : "Cancel"}
		</button>
		{#if !config.readOnly}
			<button
				type="submit"
				form="download-client-form"
				disabled={!form.state.canSubmit || form.state.isSubmitting}
				class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if form.state.isSubmitting}
					Saving…
				{:else if editing}
					Save changes
				{:else}
					Add client
				{/if}
			</button>
		{/if}
	{/snippet}
</Modal>

<Dialog
	open={deleting !== null}
	title="Delete download client '{deleting?.name ?? ''}'?"
	body="Grabs will no longer be sent to this client."
	onClose={() => (deleting = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete",
			variant: "danger",
			onClick: () => deleting && remove.mutate(deleting.name),
		},
	]}
/>
