<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Search, Pencil, Eye, Lock } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { indexerForm } from "../../lib/schemas";
	import type { Indexer, IndexerProtocol } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import IndexerForm from "../../components/settings/forms/IndexerForm.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";

	type Values = {
		name: string;
		protocol: IndexerProtocol;
		host: string;
		port: number;
		path: string;
		use_ssl: boolean;
		api_key: string;
		priority: number;
		enabled: boolean;
	};

	// Prowlarr aggregates via its native API; everything else is a Torznab feed
	// (a single tracker, or a Jackett /all aggregate). Drives the card badge.
	const PROVIDERS: Record<
		IndexerProtocol,
		{ label: string; hint: string; logo?: string }
	> = {
		prowlarr: {
			label: "Prowlarr",
			hint: "queries all indexers",
			logo: "prowlarr",
		},
		torznab: { label: "Torznab", hint: "single feed" },
	};

	const qc = useQueryClient();

	const list = createQuery<Indexer[]>(() => ({
		queryKey: ["indexers"],
		queryFn: () => api<Indexer[]>("/indexers"),
	}));

	let editing = $state<Indexer | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<Indexer, Error, Values>(() => ({
		mutationFn: (body) => {
			const payload = { ...body };
			if (editing) {
				return api<Indexer>(`/indexers/${encodeURIComponent(editing.name)}`, {
					method: "PUT",
					body: payload,
				});
			}
			return api<Indexer>("/indexers", { method: "POST", body: payload });
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok(editing ? "Indexer updated" : "Indexer added");
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(err.message),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/indexers/${encodeURIComponent(name)}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok("Indexer deleted");
		},
		onError: (err) => toast.err(err.message),
	}));

	const defaults: Values = {
		name: "",
		protocol: "torznab",
		host: "",
		port: 9696,
		path: "",
		use_ssl: false,
		api_key: "",
		priority: 25,
		enabled: true,
	};

	const form = createForm(() => ({
		defaultValues: defaults,
		validators: { onChange: indexerForm },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openCreate() {
		editing = null;
		form.reset(defaults);
		modalOpen = true;
	}

	function openEdit(i: Indexer) {
		editing = i;
		form.reset({
			name: i.name,
			protocol: i.protocol,
			host: i.host,
			port: i.port,
			path: i.path ?? "",
			use_ssl: i.use_ssl ?? false,
			api_key: "",
			priority: i.priority ?? 25,
			enabled: i.enabled,
		});
		modalOpen = true;
	}

	let deleting = $state<Indexer | null>(null);
	function onDelete(i: Indexer) {
		deleting = i;
	}

	let items = $derived(list.data ?? []);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">Indexers</h1>
			<p class="mt-1 text-sm text-fg-muted">
				Torznab indexers Streamline queries for releases.
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
			Add indexer
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
				<Search
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">No indexers configured.</p>
				<p class="mt-1 text-xs text-fg-muted">
					Add at least one Torznab indexer to start searching for
					releases.
				</p>
			</div>
		{:else}
			{#each items as i (i.name)}
				<div
					class="group relative flex flex-col gap-3 overflow-hidden rounded-lg border border-border bg-bg-elevated p-5 transition hover:border-border-strong"
				>
					<button
						type="button"
						onclick={() => openEdit(i)}
						class="flex items-start gap-3 text-left"
						aria-label="{config.readOnly ? 'View' : 'Edit'} {i.name}"
					>
						<div
							class="flex h-12 w-12 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted"
						>
							{#if PROVIDERS[i.protocol].logo}
								<BrandLogo
									name={PROVIDERS[i.protocol].logo!}
									size={26}
									ariaLabel={PROVIDERS[i.protocol].label}
								/>
							{:else}
								<Search size={24} aria-hidden="true" />
							{/if}
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate text-base font-semibold text-fg">
									{i.name}
								</span>
								{#if i.enabled}
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
								{#if i.api_key_set}
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
								{i.use_ssl ? "https" : "http"}://{i.host}:{i.port}{i.path ??
									""}
							</div>
							<div class="mt-0.5 text-[11px] text-fg-subtle">
								{PROVIDERS[i.protocol].label}
								<span class="text-fg-faint"
									>· {PROVIDERS[i.protocol].hint}</span
								>
							</div>
						</div>
					</button>
					<div
						class="mt-1 flex items-center justify-between gap-1 border-t border-border pt-3"
					>
						<TestConnectionButton endpoint="/indexers/{encodeURIComponent(i.name)}/test" />
						<div class="flex items-center gap-1">
							{#if config.readOnly}
								<button
									type="button"
									onclick={() => openEdit(i)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="View indexer"
								>
									<Eye size={16} aria-hidden="true" />
								</button>
							{:else}
								<button
									type="button"
									onclick={() => openEdit(i)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
									aria-label="Edit indexer"
								>
									<Pencil size={16} aria-hidden="true" />
								</button>
								<button
									type="button"
									onclick={() => onDelete(i)}
									class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
									aria-label="Delete indexer"
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
		? "View indexer"
		: editing
			? "Edit indexer"
			: "Add indexer"}
	size="xl"
	onClose={() => (modalOpen = false)}
>
	<form
		id="indexer-form"
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
			<IndexerForm {form} isEdit={editing !== null} />
		</fieldset>
	</form>

	{#snippet footer()}
		<div class="sm:mr-auto">
			{#if editing}
				<TestConnectionButton
					endpoint="/indexers/{encodeURIComponent(editing.name)}/test"
					size="md"
				/>
			{:else}
				<TestConnectionButton
					endpoint="/indexers/test"
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
				form="indexer-form"
				disabled={!form.state.canSubmit || form.state.isSubmitting}
				class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if form.state.isSubmitting}
					Saving…
				{:else if editing}
					Save changes
				{:else}
					Add indexer
				{/if}
			</button>
		{/if}
	{/snippet}
</Modal>

<Dialog
	open={deleting !== null}
	title="Delete indexer '{deleting?.name ?? ''}'?"
	body="Streamline will stop searching this indexer for releases."
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
