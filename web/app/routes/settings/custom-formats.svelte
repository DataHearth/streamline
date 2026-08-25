<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { Plus, Trash2, Tags, Pencil, Eye, Lock } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { customFormat } from "../../lib/schemas";
	import type { CustomFormat } from "../../lib/types";
	import ConfigFormShell from "../../components/modals/ConfigFormShell.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import CustomFormatEditor, {
		type CustomFormatDraft,
		conditionTypeLabel,
		draftFrom,
		toConditions,
	} from "../../components/settings/CustomFormatEditor.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const list = createQuery<CustomFormat[]>(() => ({
		queryKey: ["custom-formats"],
		queryFn: () => api<CustomFormat[]>("/custom-formats"),
	}));

	let editing = $state<CustomFormat | null>(null);
	let modalOpen = $state(false);
	let draft = $state<CustomFormatDraft>(draftFrom(null));

	const save = createMutation<CustomFormat, Error, CustomFormatDraft>(() => ({
		mutationFn: (d) => {
			const body = {
				name: d.name.trim(),
				description: d.description.trim() || undefined,
				conditions: toConditions(d.conditions),
			};
			if (editing) {
				return api<CustomFormat>(
					`/custom-formats/${encodeURIComponent(editing.name)}`,
					{ method: "PUT", body },
				);
			}
			return api<CustomFormat>("/custom-formats", { method: "POST", body });
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["custom-formats"] });
			toast.ok(editing ? i18n.cf_updated() : i18n.cf_created());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) =>
			api<null>(`/custom-formats/${encodeURIComponent(name)}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["custom-formats"] });
			toast.ok(i18n.cf_deleted());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	function openCreate() {
		editing = null;
		draft = draftFrom(null);
		modalOpen = true;
	}

	function openEdit(f: CustomFormat) {
		editing = f;
		draft = draftFrom(f);
		modalOpen = true;
	}

	let deleting = $state<CustomFormat | null>(null);

	// The API answers builtins first, then the user's; splitting them is what
	// lets the shipped library render without edit or delete affordances at all
	// rather than with disabled ones.
	let items = $derived(list.data ?? []);
	let builtins = $derived(items.filter((f) => f.builtin));
	let mine = $derived(items.filter((f) => !f.builtin));

	let valid = $derived(
		v.safeParse(customFormat, {
			...$state.snapshot(draft),
			name: draft.name.trim(),
		}).success,
	);

	function summary(f: CustomFormat): string {
		return f.conditions.map((c) => conditionTypeLabel(c.type)).join(" · ");
	}
</script>

<div class="mx-auto max-w-4xl">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.settings_custom_formats()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">{i18n.cf_intro()}</p>
		</div>
		<button
			type="button"
			onclick={openCreate}
			disabled={config.readOnly}
			title={config.readOnly ? READONLY_HINT : null}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			<Plus size={16} aria-hidden="true" />
			{i18n.cf_add()}
		</button>
	</header>

	{#if list.isPending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if list.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
		</p>
	{:else}
		<section class="mt-6">
			<h2 class="text-sm font-semibold text-fg">{i18n.cf_yours()}</h2>
			<div class="mt-3 space-y-3">
				{#if mine.length === 0}
					<div
						class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
					>
						<Tags size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
						<p class="mt-3 text-sm text-fg">{i18n.cf_none()}</p>
						<p class="mt-1 text-xs text-fg-muted">{i18n.cf_none_help()}</p>
					</div>
				{:else}
					{#each mine as f (f.name)}
						<div
							class="flex items-center gap-4 rounded-lg border border-border bg-bg-elevated p-4 transition hover:border-border-strong"
						>
							<button
								type="button"
								onclick={() => openEdit(f)}
								class="flex min-w-0 flex-1 items-center gap-4 text-left"
								aria-label="{config.readOnly
									? i18n.cf_view()
									: i18n.cf_edit()} {f.name}"
							>
								<div
									class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted"
								>
									<Tags size={20} aria-hidden="true" />
								</div>
								<div class="min-w-0 flex-1">
									<span class="truncate text-sm font-semibold text-fg">
										{f.name}
									</span>
									<div class="mt-1 truncate text-xs text-fg-muted">
										{#if f.description}
											{f.description}
										{:else}
											{f.conditions.length === 1
												? i18n.cf_conditions_count_one({ count: 1 })
												: i18n.cf_conditions_count_other({
														count: f.conditions.length,
													})}
											<span class="text-fg-faint">· {summary(f)}</span>
										{/if}
									</div>
								</div>
							</button>
							<div class="flex shrink-0 items-center gap-1">
								{#if config.readOnly}
									<button
										type="button"
										onclick={() => openEdit(f)}
										class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.cf_view()}
									>
										<Eye size={16} aria-hidden="true" />
									</button>
								{:else}
									<button
										type="button"
										onclick={() => openEdit(f)}
										class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
										aria-label={i18n.cf_edit()}
									>
										<Pencil size={16} aria-hidden="true" />
									</button>
									<button
										type="button"
										onclick={() => (deleting = f)}
										class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
										aria-label={i18n.cf_delete()}
									>
										<Trash2 size={16} aria-hidden="true" />
									</button>
								{/if}
							</div>
						</div>
					{/each}
				{/if}
			</div>
		</section>

		<section class="mt-8">
			<h2 class="text-sm font-semibold text-fg">{i18n.cf_builtin_section()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.cf_builtin_help()}
			</p>
			<div class="mt-3 grid gap-2 sm:grid-cols-2">
				{#each builtins as f (f.name)}
					<div class="rounded-lg border border-border bg-bg-card p-3">
						<div class="flex items-center gap-2">
							<Lock size={13} class="shrink-0 text-fg-faint" aria-hidden="true" />
							<span class="truncate font-mono text-xs font-semibold text-fg">
								{f.name}
							</span>
							<span
								class="ml-auto shrink-0 rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
							>
								{i18n.cf_builtin_badge()}
							</span>
						</div>
						<p
							class="mt-1 line-clamp-2 text-[11px] text-fg-subtle"
							title={summary(f)}
						>
							{f.description ?? summary(f)}
						</p>
					</div>
				{/each}
			</div>
		</section>
	{/if}
</div>

<ConfigFormShell
	open={modalOpen}
	title={config.readOnly
		? i18n.cf_view()
		: editing
			? i18n.cf_edit()
			: i18n.cf_add()}
	size="3xl"
	formId="custom-format-form"
	submitLabel={save.isPending
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.cf_add()}
	submitDisabled={!valid || save.isPending}
	onClose={() => (modalOpen = false)}
>
	<form
		id="custom-format-form"
		onsubmit={(e) => {
			e.preventDefault();
			if (valid) save.mutate($state.snapshot(draft));
		}}
	>
		<ReadOnlyFieldset>
			<CustomFormatEditor bind:draft isEdit={editing !== null} />
		</ReadOnlyFieldset>
	</form>
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title="Delete custom format '{deleting?.name ?? ''}'?"
	body="Quality profiles scoring it must drop it first — Streamline refuses the delete while one still references it."
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
