<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Cast, Folder, Pencil, Eye, Lock } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { mediaServerForm } from "../../lib/schemas";
	import type { MediaServer, MediaServerType } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import MediaServerForm from "../../components/settings/forms/MediaServerForm.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";

	type Values = {
		name: string;
		server_type: MediaServerType;
		host: string;
		api_key: string;
		library_section: string;
		enabled: boolean;
	};

	const qc = useQueryClient();

	const list = createQuery<{ items: MediaServer[] }>(() => ({
		queryKey: ["media-servers"],
		queryFn: () => api<{ items: MediaServer[] }>("/media-servers"),
	}));

	let editing = $state<MediaServer | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<MediaServer, Error, Values>(() => ({
		mutationFn: (body) => {
			const payload: Record<string, unknown> = {
				name: body.name,
				server_type: body.server_type,
				host: body.host,
				enabled: body.enabled,
			};
			if (body.api_key) payload.api_key = body.api_key;
			if (body.library_section) {
				payload.library_section = body.library_section;
			} else if (editing) {
				payload.library_section = null;
			}
			if (editing) {
				return api<MediaServer>(
					`/media-servers/${encodeURIComponent(editing.name)}`,
					{
						method: "PATCH",
						body: payload,
					},
				);
			}
			payload.api_key = body.api_key;
			return api<MediaServer>("/media-servers", {
				method: "POST",
				body: payload,
			});
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["media-servers"] });
			toast.ok(editing ? "Server updated" : "Server added");
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(err.message),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/media-servers/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["media-servers"] });
			toast.ok("Server deleted");
		},
		onError: (err) => toast.err(err.message),
	}));

	const defaults: Values = {
		name: "Plex",
		server_type: "plex",
		host: "https://plex.local:32400",
		api_key: "",
		library_section: "",
		enabled: true,
	};

	const form = createForm(() => ({
		defaultValues: defaults,
		validators: { onChange: mediaServerForm },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openCreate() {
		editing = null;
		form.reset(defaults);
		modalOpen = true;
	}

	function openEdit(s: MediaServer) {
		editing = s;
		form.reset({
			name: s.name,
			server_type: s.server_type,
			host: s.host,
			api_key: "",
			library_section: s.library_section ?? "",
			enabled: s.enabled,
		});
		modalOpen = true;
	}

	let deleting = $state<MediaServer | null>(null);
	function onDelete(s: MediaServer) {
		deleting = s;
	}

	let items = $derived(list.data?.items ?? []);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				Media servers
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				Plex, Jellyfin, and Emby servers Streamline notifies after import.
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<button
				type="button"
				onclick={openCreate}
				disabled={config.readOnly}
				title={config.readOnly ? READONLY_HINT : null}
				class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Plus size={16} aria-hidden="true" />
				Add server
			</button>
		</div>
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
				<Cast
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">No media servers configured.</p>
			</div>
		{:else}
			{#each items as s (s.name)}
				<div
					class="group relative flex flex-col gap-3 overflow-hidden rounded-lg border border-border bg-bg-elevated p-5 transition hover:border-border-strong"
				>
					<button
						type="button"
						onclick={() => openEdit(s)}
						class="flex items-start gap-3 text-left"
						aria-label="{config.readOnly ? 'View' : 'Edit'} {s.name}"
					>
						<div
							class="flex h-12 w-12 shrink-0 items-center justify-center rounded-md bg-bg-card"
						>
							<BrandLogo name={s.server_type} size={24} />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate text-base font-semibold text-fg">
									{s.name}
								</span>
								{#if s.enabled}
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
								{#if s.api_key_set}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										api key set
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
									>
										api key missing
									</span>
								{/if}
							</div>
							<div
								class="mt-0.5 truncate font-mono text-xs text-fg-muted"
							>
								{s.host}
							</div>
							<div class="mt-0.5 text-[11px] text-fg-subtle">
								{s.server_type}
							</div>
						</div>
					</button>
					{#if s.library_section}
						<div class="flex items-center gap-2 text-xs text-fg-muted">
							<Folder size={14} aria-hidden="true" />
							<span class="truncate">Library: {s.library_section}</span>
						</div>
					{/if}
					<div
						class="mt-1 flex items-center justify-between gap-1 border-t border-border pt-3"
					>
						<TestConnectionButton
							endpoint="/media-servers/{encodeURIComponent(s.name)}/test"
						/>
						<div class="flex items-center gap-1">
							{#if config.readOnly}
								<button
									type="button"
									onclick={() => openEdit(s)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="View server"
								>
									<Eye size={16} aria-hidden="true" />
								</button>
							{:else}
								<button
									type="button"
									onclick={() => openEdit(s)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="Edit server"
								>
									<Pencil size={16} aria-hidden="true" />
								</button>
								<button
									type="button"
									onclick={() => onDelete(s)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
									aria-label="Delete server"
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
		? "View media server"
		: editing
			? "Edit media server"
			: "Add media server"}
	size="xl"
	onClose={() => (modalOpen = false)}
>
	<form
		id="media-server-form"
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
			<MediaServerForm {form} isEdit={editing !== null} />
		</fieldset>
	</form>

	{#snippet footer()}
		<div class="sm:mr-auto">
			{#if editing}
				<TestConnectionButton
					endpoint="/media-servers/{encodeURIComponent(editing.name)}/test"
					size="md"
				/>
			{:else}
				<TestConnectionButton
					endpoint="/media-servers/test"
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
				form="media-server-form"
				disabled={!form.state.canSubmit || form.state.isSubmitting}
				class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if form.state.isSubmitting}
					Saving…
				{:else if editing}
					Save changes
				{:else}
					Add server
				{/if}
			</button>
		{/if}
	{/snippet}
</Modal>

<Dialog
	open={deleting !== null}
	title="Delete media server '{deleting?.name ?? ''}'?"
	body="Streamline will stop notifying this server about library changes."
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
