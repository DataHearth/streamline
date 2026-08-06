<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, KeyRound, AlertTriangle } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { oidcProviderCreate } from "../../lib/schemas";
	import type { OIDCProvider, OIDCProviderList } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import OIDCProviderForm from "../../components/settings/forms/OIDCProviderForm.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import ConfigModalFooter from "../../components/settings/ConfigModalFooter.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const list = createQuery<OIDCProviderList>(() => ({
		queryKey: ["config", "oidc"],
		queryFn: () => api<OIDCProviderList>("/config/oidc"),
	}));

	let modalOpen = $state(false);

	const create = createMutation<OIDCProvider, Error, typeof form.state.values>(
		() => ({
			mutationFn: (body) =>
				api<OIDCProvider>("/config/oidc", { method: "POST", body }),
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["config", "oidc"] });
				toast.ok("Provider added — restart required to apply");
				modalOpen = false;
				form.reset();
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/config/oidc/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["config", "oidc"] });
			toast.ok("Provider deleted");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: {
			name: "",
			issuer: "",
			client_id: "",
			client_secret: "",
		},
		validators: { onChange: oidcProviderCreate },
		onSubmit: ({ value }) => create.mutate(value),
	}));

	let deleting = $state<OIDCProvider | null>(null);
	function onDelete(p: OIDCProvider) {
		deleting = p;
	}

	let providers = $derived(list.data?.providers ?? []);
	let restartRequired = $derived(list.data?.restart_required ?? false);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.settings_sso()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.settings_oidc_intro()}
			</p>
		</div>
		<button
			type="button"
			onclick={() => (modalOpen = true)}
			disabled={config.readOnly}
			title={config.readOnly ? READONLY_HINT : null}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			<Plus size={16} aria-hidden="true" />
			{i18n.oidc_add_provider()}
		</button>
	</header>

	{#if restartRequired}
		<div
			class="mt-4 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
		>
			<AlertTriangle size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<div>
				<p class="font-medium">{i18n.settings_restart_required()}</p>
				<p class="mt-0.5 text-status-wanted/80">
					{i18n.settings_oidc_restart()}
				</p>
			</div>
		</div>
	{/if}

	<div class="mt-6 space-y-3">
		{#if list.isPending}
			<p class="text-sm text-fg-subtle">{i18n.common_loading()}</p>
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
			</p>
		{:else if providers.length === 0}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
			>
				<KeyRound
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">{i18n.oidc_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					Click <span class="font-medium text-fg-muted">{i18n.oidc_add_provider()}</span>
					to federate with an external IdP.
				</p>
			</div>
		{:else}
			<div class="space-y-2">
				{#each providers as p (p.name)}
					<div
						class="flex items-center gap-4 rounded-lg border border-border bg-bg-elevated p-4 transition hover:border-border-strong"
					>
						<div
							class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted"
						>
							<BrandLogo name={p.name} size={20} />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate text-sm font-semibold text-fg"
									>{p.name}</span
								>
								{#if p.client_secret_set}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										configured
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
									>
										secret missing
									</span>
								{/if}
							</div>
							<div class="mt-1 truncate font-mono text-xs text-fg-muted">
								{p.issuer}
							</div>
							<div
								class="mt-0.5 truncate font-mono text-[11px] text-fg-subtle"
							>
								client_id: {p.client_id}
							</div>
						</div>
						<button
							type="button"
							onclick={() => onDelete(p)}
							disabled={config.readOnly}
							title={config.readOnly ? READONLY_HINT : null}
							class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-fg-muted"
							aria-label={i18n.oidc_delete_provider()}
						>
							<Trash2 size={16} aria-hidden="true" />
						</button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<Modal
	open={modalOpen}
	title={i18n.oidc_add_provider_long()}
	onClose={() => (modalOpen = false)}
>
	<form
		id="oidc-provider-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<OIDCProviderForm {form} />
		</ReadOnlyFieldset>
	</form>

	{#snippet footer()}
		<ConfigModalFooter
			formId="oidc-provider-form"
			submitLabel={form.state.isSubmitting ? i18n.action_adding() : i18n.oidc_add_provider()}
			submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
			onCancel={() => (modalOpen = false)}
		/>
	{/snippet}
</Modal>

<Dialog
	open={deleting !== null}
	title="Delete OIDC provider '{deleting?.name ?? ''}'?"
	body="Users will no longer be able to sign in through this provider."
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
