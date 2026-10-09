<script lang="ts">
	import { untrack } from "svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Trash2, Gauge, Pencil, Eye, Star, BookOpen, Layers, MessageSquare, Sparkles } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import ReadOnlyFieldset from "./ReadOnlyFieldset.svelte";
	import MusicProfileForm from "./forms/MusicProfileForm.svelte";
	import BookProfileForm from "./forms/BookProfileForm.svelte";
	import {
		BOOK_PROFILE_DEFAULTS,
		MUSIC_PROFILE_DEFAULTS,
		bookProfileSchema,
		bookRequest,
		bookValues,
		formatLabel,
		kindLabel,
		musicProfileSchema,
		musicRequest,
		musicValues,
		profilesPath,
		sortTiers,
		tierLabel,
		type BookKind,
		type BookProfile,
		type MusicProfile,
		type ProfileMedia,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The music and books halves of Settings → Quality profiles: the same list,
	// row and actions as the video profiles, over /music/quality-profiles and
	// /books/quality-profiles. The page mounts one instance per tab, so `media`
	// is fixed for an instance. Music has one default; books one per kind.
	let { media }: { media: ProfileMedia } = $props();
	const isMusic = untrack(() => media) === "music";
	const base = profilesPath(untrack(() => media));
	const KINDS: { kind: BookKind; icon: typeof BookOpen }[] = [
		{ kind: "novel", icon: BookOpen },
		{ kind: "bd", icon: Layers },
		{ kind: "comic", icon: MessageSquare },
		{ kind: "manga", icon: Sparkles },
	];

	type Profile = MusicProfile | BookProfile;
	const qc = useQueryClient();
	const list = createQuery<Profile[]>(() => ({
		queryKey: ["quality-profiles", media],
		queryFn: () => api<Profile[]>(base),
	}));
	let items = $derived(list.data ?? []);

	let editing = $state<Profile | null>(null);
	let modalOpen = $state(false);
	let deleting = $state<Profile | null>(null);
	const at = (name: string) => `${base}/${encodeURIComponent(name)}`;
	const defaultKinds = (p: Profile): BookKind[] => ("default_for" in p ? p.default_for : []);
	const isDefault = (p: Profile) => ("tiers" in p ? !!p.is_default : defaultKinds(p).length > 0);
	const invalidate = () => qc.invalidateQueries({ queryKey: ["quality-profiles"] });

	const save = createMutation<Profile, Error, object>(() => ({
		mutationFn: (body) =>
			editing
				? api<Profile>(at(editing.name), { method: "PUT", body })
				: api<Profile>(base, { method: "POST", body }),
		onSuccess: () => {
			invalidate();
			toast.ok(editing ? i18n.quality_updated() : i18n.quality_created());
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));
	const remove = createMutation<null, Error, string>(() => ({
		mutationFn: (name) => api<null>(at(name), { method: "DELETE" }),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.qp_deleted());
		},
		onError: (err) => toast.err(errorText(err)),
	}));
	// Deleting the default is a 409 here too, so this is also how a profile is
	// freed for deletion.
	const makeDefault = createMutation<null, Error, { name: string; kind?: BookKind }>(() => ({
		mutationFn: ({ name, kind }) =>
			api<null>(`${at(name)}/default${kind ? `?kind=${kind}` : ""}`, { method: "POST" }),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.quality_default_set());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const musicForm = createForm(() => ({
		defaultValues: structuredClone(MUSIC_PROFILE_DEFAULTS),
		validators: { onChange: musicProfileSchema },
		onSubmit: ({ value }) => save.mutate(musicRequest(value)),
	}));
	const bookForm = createForm(() => ({
		defaultValues: structuredClone(BOOK_PROFILE_DEFAULTS),
		validators: { onChange: bookProfileSchema },
		onSubmit: ({ value }) => save.mutate(bookRequest(value)),
	}));
	const form = isMusic ? musicForm : bookForm;

	export function openCreate() {
		editing = null;
		if (isMusic) musicForm.reset(structuredClone(MUSIC_PROFILE_DEFAULTS));
		else bookForm.reset(structuredClone(BOOK_PROFILE_DEFAULTS));
		modalOpen = true;
	}
	function openEdit(p: Profile) {
		editing = p;
		if (isMusic) musicForm.reset(musicValues(p as MusicProfile));
		else bookForm.reset(bookValues(p as BookProfile));
		modalOpen = true;
	}

	// The row's second line, in the video rows' terms: what it aims for and the
	// floor under it, then for books the same per slot.
	function musicMeta(p: MusicProfile) {
		const ts = sortTiers(p.tiers);
		return { preferred: tierLabel(p.preferred), min: tierLabel(ts[ts.length - 1] ?? p.preferred) };
	}
	const more = (n: number) => (n > 1 ? ` +${n - 1}` : "");

	const formId = `${untrack(() => media)}-profile-form`;
</script>

<div class="space-y-3">
	{#if list.isPending}
		<SkeletonList variant="row" count={3} />
	{:else if list.isError}
		<p class="text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
		</p>
	{:else if items.length === 0}
		<div class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center">
			<Gauge size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
			<p class="mt-3 text-sm text-fg">{i18n.quality_none()}</p>
			<p class="mt-1 text-xs text-fg-muted">{i18n.quality_none_help()}</p>
		</div>
	{:else}
		{#each items as p (p.name)}
			<div class="flex items-center gap-4 rounded-lg border border-border bg-bg-elevated p-4 transition hover:border-border-strong">
				<button
					type="button"
					onclick={() => openEdit(p)}
					class="flex min-w-0 flex-1 items-center gap-4 text-left"
					aria-label={config.readOnly ? i18n.common_view_name({ name: p.name }) : i18n.common_edit_name({ name: p.name })}
				>
					<div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted">
						<Gauge size={20} aria-hidden="true" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
							<span class="truncate text-sm font-semibold text-fg">{p.name}</span>
							{#if "tiers" in p}
								{#if p.is_default}
									<span class="inline-flex items-center rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
										{i18n.quality_default_badge()}
									</span>
								{/if}
							{:else}
								{#each p.default_for as k (k)}
									<span class="inline-flex items-center rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
										{i18n.quality_default_kind_badge({ kind: kindLabel(k) })}
									</span>
								{/each}
							{/if}
							{#if p.upgrade_allowed}
								<span class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available">
									{i18n.qp_upgrades_on()}
								</span>
							{:else}
								<span class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted">
									{i18n.qp_locked()}
								</span>
							{/if}
						</div>
						<div class="mt-1 truncate text-xs text-fg-muted">
							{#if "tiers" in p}
								{@const m = musicMeta(p)}
								{i18n.quality_preferred_short()}
								<span class="text-fg">{m.preferred}</span> · Min
								<span class="text-fg">{m.min}</span>
							{:else}
								{formatLabel("ebook")}
								<span class="font-mono text-fg">{p.ebook.preferred}</span>{more(p.ebook.formats.length)} ·
								{formatLabel("audiobook")}
								<span class="font-mono text-fg">{p.audiobook.preferred}</span>{more(p.audiobook.formats.length)}
							{/if}
						</div>
					</div>
				</button>
				<div class="flex shrink-0 items-center gap-1">
					{#if config.readOnly}
						<button
							type="button"
							onclick={() => openEdit(p)}
							class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-fg"
							aria-label={i18n.quality_view_short()}
						>
							<Eye size={16} aria-hidden="true" />
						</button>
					{:else}
						{#if "tiers" in p}
							{#if !p.is_default}
								<button
									type="button"
									disabled={makeDefault.isPending}
									onclick={() => makeDefault.mutate({ name: p.name })}
									class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
									aria-label={i18n.quality_make_default()}
									title={i18n.quality_make_default()}
								>
									<Star size={16} aria-hidden="true" />
								</button>
							{/if}
						{:else}
							{#each KINDS as { kind, icon: Icon } (kind)}
								{#if !p.default_for.includes(kind)}
									<button
										type="button"
										disabled={makeDefault.isPending}
										onclick={() => makeDefault.mutate({ name: p.name, kind })}
										class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
										aria-label={i18n.quality_make_default_kind({ kind: kindLabel(kind) })}
										title={i18n.quality_make_default_kind({ kind: kindLabel(kind) })}
									>
										<Icon size={16} aria-hidden="true" />
									</button>
								{/if}
							{/each}
						{/if}
						<button
							type="button"
							onclick={() => openEdit(p)}
							class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-surface hover:text-fg"
							aria-label={i18n.quality_edit_short()}
						>
							<Pencil size={16} aria-hidden="true" />
						</button>
						<button
							type="button"
							disabled={isDefault(p)}
							title={isDefault(p) ? i18n.quality_default_undeletable() : null}
							onclick={() => (deleting = p)}
							class="rounded-md p-3 text-fg-muted lg:p-1.5 transition hover:bg-status-failed/10 hover:text-status-failed"
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

<ConfigFormShell
	open={modalOpen}
	title={config.readOnly ? i18n.quality_view() : editing ? i18n.quality_edit() : i18n.quality_add_long()}
	size="lg"
	{formId}
	submitLabel={form.state.isSubmitting ? i18n.common_saving() : editing ? i18n.common_save_changes() : i18n.quality_add()}
	submitDisabled={!form.state.canSubmit || form.state.isSubmitting}
	onClose={() => (modalOpen = false)}
>
	<form
		id={formId}
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			{#if isMusic}
				<MusicProfileForm form={musicForm} />
			{:else}
				<BookProfileForm form={bookForm} />
			{/if}
		</ReadOnlyFieldset>
	</form>
</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title={i18n.qp_delete_title({ name: deleting?.name ?? "" })}
	body={isMusic ? i18n.qp_delete_body_music() : i18n.qp_delete_body_books()}
	onClose={() => (deleting = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{ label: i18n.common_delete(), variant: "danger", onClick: () => deleting && remove.mutate(deleting.name) },
	]}
/>
