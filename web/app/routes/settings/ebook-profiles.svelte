<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, BookOpen, Pencil, Eye } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config, READONLY_HINT } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import { fieldErrorMessages } from "@lib/fieldErrors";
	import { EBOOK_FORMATS, ebookQualityProfile } from "@lib/schemas";
	import type { EbookFormat, EbookQualityProfile } from "@lib/types";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import Select from "@components/forms/Select.svelte";
	import TextField from "@components/forms/TextField.svelte";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import ReadOnlyFieldset from "@components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Values = {
		name: string;
		formats: EbookFormat[];
		cutoff: EbookFormat;
		upgrade_allowed: boolean;
	};

	const qc = useQueryClient();

	const list = createQuery<EbookQualityProfile[]>(() => ({
		queryKey: ["books", "ebook-quality-profiles"],
		queryFn: () => api<EbookQualityProfile[]>("/books/ebook-quality-profiles"),
	}));

	let editing = $state<EbookQualityProfile | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<EbookQualityProfile, Error, Values>(() => ({
		mutationFn: ({ name, ...rest }) => {
			if (editing) {
				return api<EbookQualityProfile>(
					`/books/ebook-quality-profiles/${encodeURIComponent(editing.name)}`,
					{ method: "PUT", body: { name: editing.name, ...rest } },
				);
			}
			return api<EbookQualityProfile>("/books/ebook-quality-profiles", {
				method: "POST",
				body: { name, ...rest },
			});
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["books", "ebook-quality-profiles"] });
			toast.ok(editing ? i18n.quality_updated() : i18n.quality_created());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/books/ebook-quality-profiles/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["books", "ebook-quality-profiles"] });
			toast.ok(i18n.qp_deleted());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const defaults: Values = {
		name: "",
		formats: ["epub", "azw3"],
		cutoff: "epub",
		upgrade_allowed: true,
	};

	const form = createForm(() => ({
		defaultValues: defaults,
		validators: { onChange: ebookQualityProfile },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openCreate() {
		editing = null;
		form.reset(defaults);
		modalOpen = true;
	}

	function openEdit(p: EbookQualityProfile) {
		editing = p;
		form.reset({
			name: p.name,
			formats: p.formats,
			cutoff: p.cutoff,
			upgrade_allowed: p.upgrade_allowed,
		});
		modalOpen = true;
	}

	// Ladder order, whatever order the boxes were ticked in: the server reads
	// the list best first.
	function withFormat(
		current: EbookFormat[],
		format: EbookFormat,
		on: boolean,
	): EbookFormat[] {
		return EBOOK_FORMATS.filter((f) =>
			f === format ? on : current.includes(f),
		);
	}

	let deleting = $state<EbookQualityProfile | null>(null);
	let items = $derived(list.data ?? []);
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.settings_ebook_profiles()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">{i18n.ebook_profiles_intro()}</p>
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
			<SkeletonList variant="row" count={3} />
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
			</p>
		{:else if items.length === 0}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
			>
				<BookOpen size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
				<p class="mt-3 text-sm text-fg">{i18n.quality_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.ebook_profiles_none_help()}
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
						aria-label={config.readOnly
							? i18n.common_view_name({ name: p.name })
							: i18n.common_edit_name({ name: p.name })}
					>
						<div
							class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted"
						>
							<BookOpen size={20} aria-hidden="true" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate text-sm font-semibold text-fg">
									{p.name}
								</span>
								{#if p.is_default}
									<span
										class="inline-flex items-center rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent"
									>
										{i18n.quality_default_badge()}
									</span>
								{/if}
								{#if p.upgrade_allowed}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										{i18n.qp_upgrades_on()}
									</span>
								{:else}
									<span
										class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
									>
										{i18n.qp_locked()}
									</span>
								{/if}
							</div>
							<div class="mt-1 truncate font-mono text-xs text-fg-muted">
								{p.formats.join(" › ")}
							</div>
						</div>
					</button>
					<div class="flex shrink-0 items-center gap-1">
						{#if config.readOnly}
							<button
								type="button"
								onclick={() => openEdit(p)}
								class="rounded-md p-3 text-fg-muted transition hover:bg-surface hover:text-fg lg:p-1.5"
								aria-label={i18n.quality_view_short()}
							>
								<Eye size={16} aria-hidden="true" />
							</button>
						{:else}
							<button
								type="button"
								onclick={() => openEdit(p)}
								class="rounded-md p-3 text-fg-muted transition hover:bg-surface hover:text-fg lg:p-1.5"
								aria-label={i18n.quality_edit_short()}
							>
								<Pencil size={16} aria-hidden="true" />
							</button>
							<button
								type="button"
								disabled={p.is_default}
								title={p.is_default ? i18n.ebook_profile_default_undeletable() : null}
								onclick={() => (deleting = p)}
								class="rounded-md p-3 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-60 lg:p-1.5"
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
	size="lg"
	formId="ebook-profile-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.quality_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id="ebook-profile-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<div class="space-y-4">
				<form.Field name="name">
					{#snippet children(field)}
						<TextField
							{field}
							label={i18n.common_name()}
							readonly={editing !== null}
						/>
					{/snippet}
				</form.Field>

				<form.Field name="formats">
					{#snippet children(field)}
						<fieldset>
							<legend
								class="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-fg"
							>
								{i18n.ebook_profile_formats()}
								<FieldLock locked={config.readOnly} />
							</legend>
							<p class="mb-2 text-xs text-fg-muted">
								{i18n.ebook_profile_formats_help()}
							</p>
							<div class="grid gap-2 sm:grid-cols-2">
								{#each EBOOK_FORMATS as format (format)}
									<Checkbox
										name="format-{format}"
										checked={field.state.value.includes(format)}
										label={format}
										onChange={(on) => {
											const next = withFormat(field.state.value, format, on);
											field.handleChange(next);
											const best = next[0];
											if (best && !next.includes(form.state.values.cutoff)) {
												form.setFieldValue("cutoff", best);
											}
										}}
									/>
								{/each}
							</div>
							{#each fieldErrorMessages(field) as msg}
								<p class="mt-1 text-xs text-status-failed">{msg}</p>
							{/each}
						</fieldset>
					{/snippet}
				</form.Field>

				<form.Field name="cutoff">
					{#snippet children(field)}
						<div>
							<Select
								label={i18n.ebook_profile_cutoff()}
								value={field.state.value}
								options={form.state.values.formats.map((f) => ({
									value: f,
									label: f,
								}))}
								onChange={(v) => field.handleChange(v)}
							/>
							<p class="mt-1 text-xs text-fg-muted">
								{i18n.ebook_profile_cutoff_help()}
							</p>
						</div>
					{/snippet}
				</form.Field>

				<form.Field name="upgrade_allowed">
					{#snippet children(field)}
						<Checkbox
							name={field.name}
							checked={field.state.value}
							onChange={(v) => field.handleChange(v)}
							label={i18n.quality_allow_upgrades()}
							description={i18n.quality_upgrades_help()}
						/>
					{/snippet}
				</form.Field>
			</div>
		</ReadOnlyFieldset>
	</form>
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title={i18n.qp_delete_title({ name: deleting?.name ?? "" })}
	body={i18n.ebook_profile_delete_body()}
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
