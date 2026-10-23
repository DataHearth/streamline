<script lang="ts">
	import { onMount } from "svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Plus, Trash2, Gauge, Pencil, Eye } from "@lucide/svelte";
	import ProfileDefaultControl from "@components/settings/ProfileDefaultControl.svelte";
	import { defaultBadge, heldLabels, videoKinds } from "@lib/profile-defaults";
	import { api, errorText } from "@lib/api";
	import { config, READONLY_HINT } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import { qualityProfile } from "@lib/schemas";
	import type { QualityProfileFull } from "@lib/types";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import QualityProfileForm, {
		TRANSCODE_DEFAULTS,
		type QualityProfileValues as Values,
	} from "@components/settings/forms/QualityProfileForm.svelte";
	import ReadOnlyFieldset from "@components/settings/ReadOnlyFieldset.svelte";
	import MediaProfilesPanel from "@components/settings/MediaProfilesPanel.svelte";
	import { onRouteQuery } from "@lib/route-query";
	import { cn } from "@lib/cn";
	import { profilesPath, type ProfileMedia } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Video, music and books keep separate lists: a resolution means nothing to
	// a FLAC or an EPUB. ?media= opens a tab directly.
	type Tab = "video" | ProfileMedia;
	let media = $state<Tab>("video");
	onMount(() =>
		onRouteQuery("/settings/quality-profiles", (p) => {
			const m = p.get("media");
			if (m === "music" || m === "books") media = m;
		}),
	);
	let panel = $state<{ openCreate: () => void } | null>(null);

	// Same keys as the panels' own lists, so the counts and the lists share one
	// request per medium.
	const musicList = createQuery<unknown[]>(() => ({
		queryKey: ["quality-profiles", "music"],
		queryFn: () => api<unknown[]>(profilesPath("music")),
	}));
	const bookList = createQuery<unknown[]>(() => ({
		queryKey: ["quality-profiles", "books"],
		queryFn: () => api<unknown[]>(profilesPath("books")),
	}));

	const qc = useQueryClient();

	const list = createQuery<QualityProfileFull[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfileFull[]>("/quality-profiles"),
	}));

	let editing = $state<QualityProfileFull | null>(null);
	let modalOpen = $state(false);

	const save = createMutation<QualityProfileFull, Error, Values>(() => ({
		mutationFn: (values) => {
			const body = toRequest(values);
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
			toast.ok(i18n.qp_deleted());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// Deleting the current default is a 409, so this is also the way to free a
	// profile for deletion — not only a preference.
	// Movies and series each have their own default.
	const kinds = videoKinds();
	const held = (p: QualityProfileFull) => p.default_for ?? (p.is_default ? ["movie", "series"] : []);
	const makeDefault = createMutation<null, Error, { name: string; kind: string | null }>(() => ({
		mutationFn: ({ name, kind }) =>
			api<null>(`/quality-profiles/${encodeURIComponent(name)}/default?media=${kind}`, {
				method: "POST",
			}),
		onSuccess: (_r, { name, kind }) => {
			qc.invalidateQueries({ queryKey: ["quality-profiles"] });
			const label = kinds.find((k) => k.value === kind)?.label;
			toast.ok(label ? i18n.quality_default_set_for({ name, kind: label }) : i18n.quality_default_set());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const defaults: Values = {
		name: "",
		preferred_resolution: "1080p",
		min_resolution: "720p",
		upgrade_allowed: true,
		allowed_codecs: [],
		formats: [],
		min_score: 0,
		upgrade_until_score: 0,
		transcode_enabled: false,
		transcode: structuredClone(TRANSCODE_DEFAULTS),
	};

	// transcode_enabled is the form's own gate, not an API field, and an
	// unchecked policy must not reach the request at all: `transcode` is absent
	// or whole. Empty `if` arrays and an empty bitrate are the API's "no rule",
	// so they go over as-is.
	function toRequest({ transcode_enabled, transcode, ...rest }: Values) {
		return transcode_enabled ? { ...rest, transcode } : rest;
	}

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
			allowed_codecs: p.allowed_codecs ?? [],
			// The API omits a zero threshold, so absent is 0 rather than unset.
			formats: (p.formats ?? []).map((f) => ({
				name: f.name,
				score: f.score ?? 0,
			})),
			min_score: p.min_score ?? 0,
			upgrade_until_score: p.upgrade_until_score ?? 0,
			transcode_enabled: p.transcode != null,
			transcode: {
				if: {
					video_codecs: p.transcode?.if?.video_codecs ?? [],
					containers: p.transcode?.if?.containers ?? [],
					max_video_bitrate: p.transcode?.if?.max_video_bitrate ?? "",
					min_video_bitrate: p.transcode?.if?.min_video_bitrate ?? "",
				},
				to: { ...TRANSCODE_DEFAULTS.to, ...(p.transcode?.to ?? {}) },
			},
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
	<header class="flex flex-wrap items-center justify-between gap-3">
		<div
			role="tablist"
			aria-label={i18n.imports_media_type()}
			class="grid w-full grid-cols-3 gap-0.5 rounded-md border border-border bg-bg-elevated p-1 sm:inline-grid sm:w-auto"
		>
			{#each [{ key: "video", label: i18n.qp_media_video(), count: items.length }, { key: "music", label: i18n.music_label(), count: musicList.data?.length }, { key: "books", label: i18n.books_label(), count: bookList.data?.length }] as t (t.key)}
				{@const active = media === t.key}
				<button
					type="button"
					role="tab"
					aria-selected={active}
					onclick={() => (media = t.key === "music" || t.key === "books" ? t.key : "video")}
					class={cn(
						"inline-flex h-10 items-center justify-center gap-2 rounded-sm px-3 text-[13px] font-medium transition lg:h-8 lg:text-[12.5px]",
						active ? "bg-bg-card text-fg shadow-[var(--shadow-1)]" : "text-fg-muted hover:text-fg",
					)}
				>
					{t.label}
					{#if t.count !== undefined}
						<span class="rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular text-fg-faint">{t.count}</span>
					{/if}
				</button>
			{/each}
		</div>
		<button
			type="button"
			onclick={() => (media === "video" ? openCreate() : panel?.openCreate())}
			disabled={config.readOnly}
			title={config.readOnly ? READONLY_HINT : null}
			class="inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 py-2.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto sm:py-2"
		>
			<Plus size={16} aria-hidden="true" />
			{i18n.quality_add()}
		</button>
	</header>
	<p class="mt-4 text-sm text-fg-muted">
		{media === "music" ? i18n.qp_intro_music() : media === "books" ? i18n.qp_intro_books() : i18n.quality_intro()}
	</p>

	{#if media !== "video"}
		<div class="mt-5">
			{#key media}
				<MediaProfilesPanel media={media === "music" ? "music" : "books"} bind:this={panel} />
			{/key}
		</div>
	{:else}
	<div class="mt-5 space-y-3">
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
				<Gauge size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
				<p class="mt-3 text-sm text-fg">{i18n.quality_none()}</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.quality_none_help()}
				</p>
			</div>
		{:else}
			{#each items as p (p.name)}
				<div
					class="flex items-center gap-3 rounded-lg border border-border bg-bg-elevated px-3.5 py-4 transition hover:border-border-strong md:gap-4 md:p-4"
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
							class="hidden h-10 w-10 shrink-0 items-center justify-center rounded-md bg-bg-card text-fg-muted md:flex"
						>
							<Gauge size={20} aria-hidden="true" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
								<span class="truncate text-sm font-semibold text-fg">
									{p.name}
								</span>
								{#if held(p).length}
									<span class="inline-flex items-center rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
										{defaultBadge(kinds, held(p))}
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
					<div class="flex shrink-0 items-center gap-0.5 md:gap-1">
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
							<ProfileDefaultControl
								name={p.name}
								{kinds}
								held={held(p)}
								busy={makeDefault.isPending}
								onMake={(kind) => makeDefault.mutate({ name: p.name, kind })}
							/>
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
								disabled={held(p).length > 0}
								title={held(p).length ? i18n.quality_default_undeletable_for({ kinds: heldLabels(kinds, held(p)) }) : null}
								onclick={() => onDelete(p)}
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
	{/if}
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
			<QualityProfileForm
				{form}
				isCreate={editing === null}
				policyPersisted={editing?.transcode != null}
			/>
		</ReadOnlyFieldset>
	</form>

</ConfigFormShell>

<Dialog
	open={deleting !== null}
	title={i18n.qp_delete_title({ name: deleting?.name ?? "" })}
	body={i18n.qp_delete_body()}
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
