<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Cast, Folder, Pencil, Eye } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { mediaServerForm } from "../../lib/schemas";
	import type { MediaServer, MediaServerType } from "../../lib/types";
	import ConfigFormShell from "../../components/modals/ConfigFormShell.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import MediaServerForm from "../../components/settings/forms/MediaServerForm.svelte";
	import TestConnectionButton from "../../components/settings/TestConnectionButton.svelte";
	import BrandLogo from "../../components/settings/BrandLogo.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Values = {
		name: string;
		server_type: MediaServerType;
		host: string;
		api_key: string;
		library_section: string;
		library_section_tv: string;
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
			// "" is the clear-the-field signal; null means "unchanged" server-side.
			for (const k of ["library_section", "library_section_tv"] as const) {
				if (body[k]) payload[k] = body[k];
				else if (editing) payload[k] = "";
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
			toast.ok(editing ? i18n.mediaserver_updated() : i18n.mediaserver_added());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
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
		onError: (err) => toast.err(errorText(err)),
	}));

	const defaults: Values = {
		name: "Plex",
		server_type: "plex",
		host: "https://plex.local:32400",
		api_key: "",
		library_section: "",
		library_section_tv: "",
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
			library_section_tv: s.library_section_tv ?? "",
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
				{i18n.settings_media_servers()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.mediaserver_intro()}
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
				{i18n.mediaserver_add()}
			</button>
		</div>
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
				<Cast
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">{i18n.mediaserver_none()}</p>
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
					{#if s.library_section || s.library_section_tv}
						<div class="flex items-center gap-2 text-xs text-fg-muted">
							<Folder size={14} aria-hidden="true" />
							<span class="truncate">
								{[
									s.library_section && `Movies: ${s.library_section}`,
									s.library_section_tv && `TV: ${s.library_section_tv}`,
								]
									.filter(Boolean)
									.join(" · ")}
							</span>
						</div>
					{/if}
					<TestConnectionButton
						endpoint="/media-servers/{encodeURIComponent(s.name)}/test"
						variant="card"
					>
						{#snippet trailing()}
							<div class="flex shrink-0 items-center gap-1">
								{#if config.readOnly}
									<button
										type="button"
										onclick={() => openEdit(s)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.mediaserver_view_short()}
									>
										<Eye size={16} aria-hidden="true" />
									</button>
								{:else}
									<button
										type="button"
										onclick={() => openEdit(s)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.mediaserver_edit_short()}
									>
										<Pencil size={16} aria-hidden="true" />
									</button>
									<button
										type="button"
										onclick={() => onDelete(s)}
										class="grid h-11 w-11 lg:h-9 lg:w-9 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
										aria-label={i18n.mediaserver_delete()}
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
		? i18n.mediaserver_view()
		: editing
			? i18n.mediaserver_edit()
			: i18n.mediaserver_add_long()}
	size="xl"
	formId="media-server-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.mediaserver_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id="media-server-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<MediaServerForm {form} isEdit={editing !== null} />
		</ReadOnlyFieldset>
	</form>

	{#snippet test(variant)}
		{#if editing}
			<TestConnectionButton
				endpoint="/media-servers/{encodeURIComponent(editing.name)}/test"
				size="md"
				{variant}
			/>
		{:else}
			<TestConnectionButton
				endpoint="/media-servers/test"
				body={() => form.state.values}
				size="md"
				{variant}
			/>
		{/if}
	{/snippet}
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title="Delete media server '{deleting?.name ?? ''}'?"
	body="Streamline will stop notifying this server about library changes."
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
