<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Download, Pencil, Eye, Zap, Info } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { downloadClientForm, builtinClientForm } from "../../lib/schemas";
	import type {
		DownloadClient,
		DownloadClientType,
		DownloadClientAuth,
	} from "../../lib/types";
	import ConfigFormShell from "../../components/modals/ConfigFormShell.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import DownloadClientForm from "../../components/settings/forms/DownloadClientForm.svelte";
	import BuiltinClientForm from "../../components/settings/forms/BuiltinClientForm.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
		// See DownloadClientForm.svelte: undefined represents a cleared input,
		// distinct from the 0 that Number("") would otherwise produce.
		priority: number | undefined;
		enabled: boolean;
	};

	type BuiltinValues = {
		download_dir: string;
		bind_interface: string;
		listen_port: number;
		max_download_kbps: number;
		max_upload_kbps: number;
		seed_ratio: number;
		seed_time: string;
		disable_dht: boolean;
		enabled: boolean;
	};

	const qc = useQueryClient();

	const list = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
	}));

	// The builtin engine is a normal download-clients entry (client_type
	// "builtin"); its config + runtime state are derived from the same list.
	// No dedicated /download-clients/builtin read endpoint.
	let builtinCfg = $derived(
		(list.data ?? []).find((c) => c.client_type === "builtin") ?? null,
	);
	// External clients render below; the builtin entry has its own card/CTA.
	let items = $derived((list.data ?? []).filter((c) => c.client_type !== "builtin"));

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
			toast.ok(editing ? i18n.dlclient_updated() : i18n.dlclient_added());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
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
		onError: (err) => toast.err(errorText(err)),
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

	// ── Built-in engine form / modal ──────────────────────────────────────
	const builtinDefaults: BuiltinValues = {
		download_dir: "/data/torrents",
		bind_interface: "",
		listen_port: 6881,
		max_download_kbps: 0,
		max_upload_kbps: 0,
		seed_ratio: 2.0,
		seed_time: "72h",
		disable_dht: false,
		enabled: true,
	};
	let builtinModalOpen = $state(false);
	let builtinIsEdit = $state(false);
	let deletingBuiltin = $state(false);

	// The builtin entry is created/updated through the name-keyed CRUD with a
	// fixed name "builtin" and client_type "builtin".
	const saveBuiltin = createMutation<DownloadClient, Error, BuiltinValues>(
		() => ({
			mutationFn: (body) => {
				const payload = { ...body, name: "builtin", client_type: "builtin" };
				if (builtinIsEdit) {
					return api<DownloadClient>("/download-clients/builtin", {
						method: "PUT",
						body: payload,
					});
				}
				return api<DownloadClient>("/download-clients", {
					method: "POST",
					body: payload,
				});
			},
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["download-clients"] });
				toast.ok("Built-in client saved — changes apply after restart");
				builtinModalOpen = false;
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	const removeBuiltin = createMutation<null, Error, void>(() => ({
		mutationFn: () =>
			api<null>("/download-clients/builtin", { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["download-clients"] });
			toast.ok("Built-in client removed");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const builtinForm = createForm(() => ({
		defaultValues: builtinDefaults,
		validators: { onChange: builtinClientForm },
		onSubmit: ({ value }) => saveBuiltin.mutate(value),
	}));

	function openBuiltinCreate() {
		builtinIsEdit = false;
		builtinForm.reset(builtinDefaults);
		builtinModalOpen = true;
	}
	function openBuiltinEdit() {
		if (!builtinCfg) return;
		builtinIsEdit = true;
		builtinForm.reset({
			download_dir: builtinCfg.download_dir ?? "",
			bind_interface: builtinCfg.bind_interface ?? "",
			// The read view omits zero-values; fall back to the semantic zeros
			// (0 = auto port, 0 = unlimited ratio, "" = unlimited time) — never
			// the create defaults, or saving an untouched form silently pins them.
			listen_port: builtinCfg.listen_port ?? 0,
			max_download_kbps: builtinCfg.max_download_kbps ?? 0,
			max_upload_kbps: builtinCfg.max_upload_kbps ?? 0,
			seed_ratio: builtinCfg.seed_ratio ?? 0,
			seed_time: builtinCfg.seed_time ?? "",
			disable_dht: builtinCfg.disable_dht ?? false,
			enabled: builtinCfg.enabled,
		});
		builtinModalOpen = true;
	}

	let builtinSubtitle = $derived.by(() => {
		if (!builtinCfg) return "";
		const port = builtinCfg.port_bound || builtinCfg.listen_port || "auto";
		const iface = builtinCfg.interface_bound || builtinCfg.bind_interface;
		const net = iface ? `${iface} · port ${port}` : `port ${port}`;
		const ratio = builtinCfg.seed_ratio ?? 0;
		const seed =
			ratio > 0 ? `seed to ratio ${ratio.toFixed(1)}` : "unlimited seeding";
		return i18n.dlclient_builtin_summary({ net, seed });
	});
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.settings_download_clients()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.dlclient_intro()}
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
			{i18n.dlclient_add()}
		</button>
	</header>

	<div class="mt-6 grid gap-3 sm:grid-cols-2">
		<!-- Built-in engine: always first. Card when configured, CTA otherwise. -->
		{#if builtinCfg}
			<div
				class="group relative col-span-full flex flex-col gap-3 overflow-hidden rounded-lg border border-accent-line bg-bg-elevated p-5 transition hover:border-accent"
			>
				<button
					type="button"
					onclick={openBuiltinEdit}
					class="flex items-start gap-3 text-left"
					aria-label="{config.readOnly ? 'View' : 'Edit'} built-in client"
				>
					<div
						class="flex h-12 w-12 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent"
					>
						<Zap size={24} aria-hidden="true" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="flex flex-wrap items-center gap-2">
							<span class="truncate text-base font-semibold text-fg">
								{i18n.builtin_client()}
							</span>
							{#if builtinCfg.enabled}
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
							{#if builtinCfg.running}
								<span
									class="inline-flex items-center gap-1 rounded-full bg-status-seeding/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-seeding"
								>
									<span
										class="h-1.5 w-1.5 rounded-full bg-status-seeding motion-safe:animate-pulse"
									></span>
									running
								</span>
							{:else}
								<span
									class="inline-flex items-center rounded-full bg-status-paused/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-paused"
								>
									stopped
								</span>
							{/if}
						</div>
						<div class="mt-0.5 truncate font-mono text-xs text-fg-muted">
							{builtinSubtitle}
						</div>
						<div class="mt-0.5 truncate text-[11px] text-fg-subtle">
							{builtinCfg.download_dir}
						</div>
					</div>
				</button>
				<div
					class="mt-1 flex items-center justify-between gap-1 border-t border-border pt-3"
				>
					<span class="text-xs text-fg-subtle">
						{#if builtinCfg.running}
							Engine running · port {builtinCfg.port_bound} bound{#if builtinCfg.interface_bound} · via {builtinCfg.interface_bound}{/if}
						{:else}
							Engine stopped
						{/if}
					</span>
					<div class="flex items-center gap-1">
						{#if config.readOnly}
							<button
								type="button"
								onclick={openBuiltinEdit}
								class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-fg"
								aria-label={i18n.builtin_view()}
							>
								<Eye size={16} aria-hidden="true" />
							</button>
						{:else}
							<button
								type="button"
								onclick={openBuiltinEdit}
								class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-fg"
								aria-label={i18n.builtin_edit()}
							>
								<Pencil size={16} aria-hidden="true" />
							</button>
							<button
								type="button"
								onclick={() => (deletingBuiltin = true)}
								class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-status-failed/10 hover:text-status-failed"
								aria-label={i18n.builtin_remove()}
							>
								<Trash2 size={16} aria-hidden="true" />
							</button>
						{/if}
					</div>
				</div>
			</div>
		{:else if !list.isPending}
			<div
				class="col-span-full flex flex-wrap items-center gap-4 rounded-lg border border-dashed border-accent-line bg-accent-soft/30 p-5"
			>
				<div
					class="grid h-12 w-12 shrink-0 place-items-center rounded-md bg-accent-soft text-accent"
				>
					<Zap size={24} aria-hidden="true" />
				</div>
				<div class="min-w-0 flex-1">
					<p class="text-sm font-semibold text-fg">
						{i18n.builtin_run_inside()}
					</p>
					<p class="mt-0.5 text-xs text-fg-muted">
						{i18n.dlclients_builtin_help()}
					</p>
				</div>
				<button
					type="button"
					onclick={openBuiltinCreate}
					disabled={config.readOnly}
					title={config.readOnly ? READONLY_HINT : null}
					class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
				>
					<Zap size={15} aria-hidden="true" />
					{i18n.builtin_enable()}
				</button>
			</div>
		{/if}

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
				<Download
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">{i18n.dlclient_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.dlclients_empty_help()}
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
					<TestConnectionButton
						endpoint="/download-clients/{encodeURIComponent(c.name)}/test"
						variant="card"
					>
						{#snippet trailing()}
							<div class="flex shrink-0 items-center gap-1">
								{#if config.readOnly}
									<button
										type="button"
										onclick={() => openEdit(c)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.dlclient_view_short()}
									>
										<Eye size={16} aria-hidden="true" />
									</button>
								{:else}
									<button
										type="button"
										onclick={() => openEdit(c)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.dlclient_edit_short()}
									>
										<Pencil size={16} aria-hidden="true" />
									</button>
									<button
										type="button"
										onclick={() => onDelete(c)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
										aria-label={i18n.dlclient_delete()}
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
		? i18n.dlclient_view()
		: editing
			? i18n.dlclient_edit()
			: i18n.dlclient_add_long()}
	size="xl"
	formId="download-client-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.dlclient_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id="download-client-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<DownloadClientForm {form} isEdit={editing !== null} />
		</ReadOnlyFieldset>
	</form>

	{#snippet test(variant)}
		{#if editing}
			<TestConnectionButton
				endpoint="/download-clients/{encodeURIComponent(editing.name)}/test"
				size="md"
				{variant}
			/>
		{:else}
			<TestConnectionButton
				endpoint="/download-clients/test"
				body={() => form.state.values}
				size="md"
				{variant}
			/>
		{/if}
	{/snippet}
</ConfigFormShell>

<!-- Built-in engine modal: no host/port/auth, no Test connection. -->
<ConfigFormShell
	open={builtinModalOpen}
	title={config.readOnly
		? i18n.builtin_view()
		: builtinIsEdit
			? i18n.builtin_client()
			: i18n.builtin_enable()}
	size="xl"
	formId="builtin-client-form"
	submitLabel={builtinForm.state.isSubmitting
		? i18n.common_saving()
		: builtinIsEdit
			? i18n.common_save_changes()
			: i18n.dlclient_enable()}
	submitDisabled={!builtinForm.state.canSubmit ||
		builtinForm.state.isSubmitting}
	onClose={() => (builtinModalOpen = false)}
>
	<form
		id="builtin-client-form"
		onsubmit={(e) => {
			e.preventDefault();
			builtinForm.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<BuiltinClientForm
				form={builtinForm}
				isEdit={builtinIsEdit}
				listenPortOverride={builtinCfg?.listen_port_override}
			/>
		</ReadOnlyFieldset>
	</form>

	{#snippet note()}
		<div class="flex items-center gap-1.5 text-xs text-fg-subtle sm:mr-auto">
			<Info size={13} aria-hidden="true" />
			<span>{i18n.settings_changes_after_restart()}</span>
		</div>
	{/snippet}
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title="Delete download client '{deleting?.name ?? ''}'?"
	body="Grabs will no longer be sent to this client."
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

<Dialog
	open={deletingBuiltin}
	title={i18n.builtin_remove_confirm()}
	body="The engine stops managing torrents and won't start on next restart. Downloaded files on disk are left in place."
	onClose={() => (deletingBuiltin = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_remove(),
			variant: "danger",
			onClick: () => removeBuiltin.mutate(),
		},
	]}
/>
