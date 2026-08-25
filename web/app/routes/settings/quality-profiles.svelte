<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Gauge, Pencil, Eye } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { qualityProfile } from "../../lib/schemas";
	import type { QualityProfileFull } from "../../lib/types";
	import ConfigFormShell from "../../components/modals/ConfigFormShell.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import QualityProfileForm, {
		type QualityProfileValues as Values,
	} from "../../components/settings/forms/QualityProfileForm.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const list = createQuery<QualityProfileFull[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfileFull[]>("/quality-profiles"),
	}));

	let editing = $state<QualityProfileFull | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<QualityProfileFull, Error, Values>(() => ({
		mutationFn: (body) => {
			if (editing) {
				return api<QualityProfileFull>(
					`/quality-profiles/${encodeURIComponent(editing.name)}`,
					{ method: "PUT", body },
				);
			}
			return api<QualityProfileFull>("/quality-profiles", {
				method: "POST",
				body,
			});
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["quality-profiles"] });
			toast.ok(editing ? i18n.quality_updated() : i18n.quality_created());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/quality-profiles/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["quality-profiles"] });
			toast.ok("Profile deleted");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const defaults: Values = {
		name: "",
		preferred_resolution: "1080p",
		min_resolution: "720p",
		upgrade_allowed: true,
		replace_whole_season: false,
		allowed_codecs: [],
		formats: [],
		min_score: 0,
		upgrade_until_score: 0,
	};

	const form = createForm(() => ({
		defaultValues: defaults,
		validators: { onChange: qualityProfile },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openCreate() {
		editing = null;
		form.reset(defaults);
		modalOpen = true;
	}

	function openEdit(p: QualityProfileFull) {
		editing = p;
		form.reset({
			name: p.name,
			preferred_resolution: p.preferred_resolution,
			min_resolution: p.min_resolution,
			upgrade_allowed: p.upgrade_allowed,
			replace_whole_season: p.replace_whole_season,
			allowed_codecs: p.allowed_codecs ?? [],
			// The API omits a zero threshold, so absent is 0 rather than unset.
			formats: (p.formats ?? []).map((f) => ({
				name: f.name,
				score: f.score ?? 0,
			})),
			min_score: p.min_score ?? 0,
			upgrade_until_score: p.upgrade_until_score ?? 0,
		});
		modalOpen = true;
	}

	let deleting = $state<QualityProfileFull | null>(null);
	function onDelete(p: QualityProfileFull) {
		deleting = p;
	}

	let items = $derived(list.data ?? []);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.settings_quality_profiles()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				{i18n.quality_intro()}
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
			{i18n.quality_add()}
		</button>
	</header>

	<div class="mt-6 space-y-3">
		{#if list.isPending}
			<p class="text-sm text-fg-subtle">{i18n.common_loading()}</p>
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
			</p>
		{:else if items.length === 0}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
			>
				<Gauge size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
				<p class="mt-3 text-sm text-fg">{i18n.quality_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.quality_none_help()}
				</p>
			</div>
		{:else}
			{#each items as p (p.name)}
				<div
					class="flex items-center gap-4 rounded-lg border border-border bg-bg-elevated p-4 transition hover:border-border-strong"
				>
					<button
						type="button"
						onclick={() => openEdit(p)}
						class="flex min-w-0 flex-1 items-center gap-4 text-left"
						aria-label="{config.readOnly ? 'View' : 'Edit'} {p.name}"
					>
						<div
							class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted"
						>
							<Gauge size={20} aria-hidden="true" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate text-sm font-semibold text-fg">
									{p.name}
								</span>
								{#if p.upgrade_allowed}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										upgrades on
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
									>
										locked
									</span>
								{/if}
							</div>
							<div class="mt-1 truncate text-xs text-fg-muted">
								{i18n.quality_preferred_short()}
								<span class="font-mono text-fg"
									>{p.preferred_resolution}</span
								> · Min
								<span class="font-mono text-fg">{p.min_resolution}</span>
								{#if (p.formats?.length ?? 0) > 0}
									·
									{p.formats?.length === 1
										? i18n.quality_formats_count_one({ count: 1 })
										: i18n.quality_formats_count_other({
												count: p.formats?.length ?? 0,
											})}
								{/if}
								{#if (p.min_score ?? 0) !== 0}
									· {i18n.quality_min_score()}
									<span class="font-mono text-fg">{p.min_score}</span>
								{/if}
							</div>
						</div>
					</button>
					<div class="flex shrink-0 items-center gap-1">
						{#if config.readOnly}
							<button
								type="button"
								onclick={() => openEdit(p)}
								class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
								aria-label={i18n.quality_view_short()}
							>
								<Eye size={16} aria-hidden="true" />
							</button>
						{:else}
							<button
								type="button"
								onclick={() => openEdit(p)}
								class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
								aria-label={i18n.quality_edit_short()}
							>
								<Pencil size={16} aria-hidden="true" />
							</button>
							<button
								type="button"
								onclick={() => onDelete(p)}
								class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
								aria-label={i18n.quality_delete()}
							>
								<Trash2 size={16} aria-hidden="true" />
							</button>
						{/if}
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>

<ConfigFormShell
	open={modalOpen}
	title={config.readOnly
		? i18n.quality_view()
		: editing
			? i18n.quality_edit()
			: i18n.quality_add_long()}
	size="xl"
	formId="quality-profile-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.quality_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id="quality-profile-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<QualityProfileForm {form} isCreate={editing === null} />
		</ReadOnlyFieldset>
	</form>

</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title="Delete quality profile '{deleting?.name ?? ''}'?"
	body="Movies using it will fall back to the default profile."
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
