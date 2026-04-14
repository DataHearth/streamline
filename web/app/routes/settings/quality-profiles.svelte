<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Gauge, Pencil } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { qualityProfile } from "../../lib/schemas";
	import type { QualityProfileFull, Resolution } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import QualityProfileForm from "../../components/settings/forms/QualityProfileForm.svelte";

	type Values = {
		name: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		upgrade_allowed: boolean;
	};

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
			toast.ok(editing ? "Profile updated" : "Profile created");
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(err.message),
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
		onError: (err) => toast.err(err.message),
	}));

	const defaults: Values = {
		name: "",
		preferred_resolution: "1080p",
		min_resolution: "720p",
		upgrade_allowed: true,
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
				Quality profiles
			</h1>
			<p class="mt-1 text-sm text-fg-muted">
				Resolution ranges Streamline accepts when grabbing releases.
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
			Add profile
		</button>
	</header>

	<div class="mt-6 space-y-3">
		{#if list.isPending}
			<p class="text-sm text-fg-subtle">Loading…</p>
		{:else if list.isError}
			<p class="text-sm text-status-failed">
				Failed to load: {list.error?.message}
			</p>
		{:else if items.length === 0}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
			>
				<Gauge size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
				<p class="mt-3 text-sm text-fg">No quality profiles yet.</p>
				<p class="mt-1 text-xs text-fg-muted">
					Create one to control which release qualities Streamline grabs.
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
						aria-label="Edit {p.name}"
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
								Preferred
								<span class="font-mono text-fg"
									>{p.preferred_resolution}</span
								> · Min
								<span class="font-mono text-fg">{p.min_resolution}</span>
							</div>
						</div>
					</button>
					<div class="flex shrink-0 items-center gap-1">
						<button
							type="button"
							onclick={() => openEdit(p)}
							class="rounded-md p-1.5 text-fg-muted transition hover:bg-surface hover:text-fg"
							aria-label="Edit profile"
						>
							<Pencil size={16} aria-hidden="true" />
						</button>
						<button
							type="button"
							onclick={() => onDelete(p)}
							disabled={config.readOnly}
							title={config.readOnly ? READONLY_HINT : null}
							class="rounded-md p-1.5 text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-fg-muted"
							aria-label="Delete profile"
						>
							<Trash2 size={16} aria-hidden="true" />
						</button>
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>

<Modal
	open={modalOpen}
	title={editing ? "Edit quality profile" : "Add quality profile"}
	size="md"
	onClose={() => (modalOpen = false)}
>
	<form
		id="quality-profile-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<QualityProfileForm {form} />
	</form>

	{#snippet footer()}
		<button
			type="button"
			onclick={() => (modalOpen = false)}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
		>
			Cancel
		</button>
		<button
			type="submit"
			form="quality-profile-form"
			disabled={config.readOnly || !form.state.canSubmit || form.state.isSubmitting}
			class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{#if form.state.isSubmitting}
				Saving…
			{:else if editing}
				Save changes
			{:else}
				Add profile
			{/if}
		</button>
	{/snippet}
</Modal>

<Dialog
	open={deleting !== null}
	title="Delete quality profile '{deleting?.name ?? ''}'?"
	body="Movies using it will fall back to the default profile."
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
