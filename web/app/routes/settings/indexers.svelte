<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Search, Pencil, Eye } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { indexerForm } from "../../lib/schemas";
	import type { Indexer, IndexerProtocol } from "../../lib/types";
	import ConfigFormShell from "../../components/modals/ConfigFormShell.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import IndexerForm from "../../components/settings/forms/IndexerForm.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
			toast.ok(editing ? i18n.indexer_updated() : i18n.indexer_added());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/indexers/${encodeURIComponent(name)}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok("Indexer deleted");
		},
		onError: (err) => toast.err(errorText(err)),
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
			<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_indexers()}</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.indexer_intro()}
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
			{i18n.indexer_add()}
		</button>
	</header>

	<div class="mt-6 grid gap-3 sm:grid-cols-2">
		{#if list.isPending}
			<p class="text-sm text-fg-subtle">{i18n.common_loading()}</p>
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
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
				<p class="mt-3 text-sm text-fg">{i18n.indexer_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.indexers_empty_help()}
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
					<TestConnectionButton
						endpoint="/indexers/{encodeURIComponent(i.name)}/test"
						variant="card"
					>
						{#snippet trailing()}
							<div class="flex shrink-0 items-center gap-1">
								{#if config.readOnly}
									<button
										type="button"
										onclick={() => openEdit(i)}
										class="grid h-9 w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.indexer_view()}
									>
										<Eye size={16} aria-hidden="true" />
									</button>
								{:else}
									<button
										type="button"
										onclick={() => openEdit(i)}
										class="grid h-9 w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.indexer_edit()}
									>
										<Pencil size={16} aria-hidden="true" />
									</button>
									<button
										type="button"
										onclick={() => onDelete(i)}
										class="grid h-9 w-9 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
										aria-label={i18n.indexer_delete()}
									>
										<Trash2 size={16} aria-hidden="true" />
									</button>
								{/if}
							</div>
						{/snippet}
					</TestConnectionButton>
				</div>
			{/each}
		{/if}
	</div>
</div>

<ConfigFormShell
	open={modalOpen}
	title={config.readOnly
		? i18n.indexer_view()
		: editing
			? i18n.indexer_edit()
			: i18n.indexer_add()}
	size="xl"
	formId="indexer-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.indexer_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id="indexer-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<IndexerForm {form} isEdit={editing !== null} />
		</ReadOnlyFieldset>
	</form>

	{#snippet test(variant)}
		{#if editing}
			<TestConnectionButton
				endpoint="/indexers/{encodeURIComponent(editing.name)}/test"
				size="md"
				{variant}
			/>
		{:else}
			<TestConnectionButton
				endpoint="/indexers/test"
				body={() => form.state.values}
				size="md"
				{variant}
			/>
		{/if}
	{/snippet}
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title="Delete indexer '{deleting?.name ?? ''}'?"
	body="Streamline will stop searching this indexer for releases."
	onClose={() => (deleting = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_delete(),
			variant: "danger",
			onClick: () => deleting && remove.mutate(deleting.name),
		},
	]}
/>
