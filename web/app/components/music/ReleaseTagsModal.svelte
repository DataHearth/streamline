<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { PenLine } from "@lucide/svelte";
	import Modal from "@components/modals/Modal.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import TagOverridesEditor from "./TagOverridesEditor.svelte";
	import { api, errorText } from "@lib/api";
	import { createAction, MUSIC, updateFailed } from "@lib/actions";
	import { cn } from "@lib/cn";
	import { differing, type AlbumTags, type TagFile, type TagFileState, type TagOverrides } from "@lib/music-tags";
	import type { Release } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What the tag writer does with one release's files: each file read and
	// compared, key by key, with what a write would leave in it; the
	// release's managed switch; Write tags now; and the tags set by hand, for
	// every file or for one track.
	let { release, onClose }: { release: Release | null; onClose: () => void } = $props();

	let id = $derived(release?.id);
	let key = $derived(["music", "album-tags", id]);
	const tagsQuery = createQuery<AlbumTags>(() => ({
		queryKey: key,
		queryFn: () => api<AlbumTags>(`/music/albums/${id}/tags`),
		enabled: !!id,
		// A write in the background lands within seconds.
		refetchInterval: (q) => (q.state.data?.files.some((f) => f.state === "pending") ? 3000 : false),
	}));
	let tags = $derived(tagsQuery.data);

	let fileId = $state<number | null>(null);
	$effect(() => {
		if (!release) fileId = null;
	});
	let file = $derived<TagFile | undefined>(
		tags ? (tags.files.find((f) => f.media_file_id === fileId) ?? tags.files[0]) : undefined,
	);
	let showAll = $state(false);
	let rows = $derived(file ? file.keys.filter((k) => showAll || k.status === "differs") : []);
	let off = $derived(tags?.mode === "off");
	let inStep = $derived(tags ? tags.files.filter((f) => !f.read_error && differing(f) === 0).length : 0);

	const refresh = () => [key, MUSIC];
	const saveAlbum = createAction<{ tags_managed?: boolean; tag_overrides?: TagOverrides }>(() => ({
		run: (body) => api(`/music/albums/${id}`, { method: "PATCH", body }),
		refresh: refresh(),
		failed: updateFailed,
		succeeded: (body) => (body.tag_overrides ? i18n.music_tags_saved() : undefined),
	}));
	const saveTrack = createAction<{ track: number; overrides: TagOverrides }>(() => ({
		run: ({ track, overrides }) => api(`/music/tracks/${track}`, { method: "PATCH", body: { tag_overrides: overrides } }),
		refresh: [key],
		failed: updateFailed,
		succeeded: () => i18n.music_tags_saved(),
	}));
	const writeNow = createAction<void, { queued: number }>(() => ({
		run: () => api<{ queued: number }>(`/music/albums/${id}/tags/apply`, { method: "POST" }),
		refresh: [key],
		failed: updateFailed,
		succeeded: (_, d) =>
			d.queued === 1
				? i18n.music_tags_panel_written_one({ count: String(d.queued) })
				: i18n.music_tags_panel_written_other({ count: String(d.queued) }),
	}));

	const STATE: Record<TagFileState, () => string> = {
		untagged: () => i18n.music_tags_state_untagged(),
		pending: () => i18n.music_tags_state_pending(),
		deferred: () => i18n.music_tags_state_deferred(),
		written: () => i18n.music_tags_state_written(),
		failed: () => i18n.music_tags_state_failed(),
	};
	const pad = (n: number) => String(n).padStart(2, "0");
	let fileOptions = $derived(
		(tags?.files ?? []).map((f) => ({
			value: String(f.media_file_id),
			label: `${f.disc > 1 ? `${f.disc}-` : ""}${pad(f.position)} · ${f.title}`,
			hint: differing(f) ? String(differing(f)) : undefined,
		})),
	);
	const caps = "font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted";
	const values = (v: string[]) => (v.length ? v.join(" ; ") : "—");
</script>

<Modal open={!!release} title={i18n.music_tags_panel_title({ title: release?.title ?? "" })} size="3xl" {onClose}>
	{#if tagsQuery.isPending}
		<div class="flex flex-col gap-3">
			<Skeleton h={14} w="60%" />
			<Skeleton h={120} />
		</div>
	{:else if tagsQuery.isError}
		<p class="text-sm text-status-failed">{errorText(tagsQuery.error)}</p>
	{:else if tags}
		<div class="flex flex-col gap-6">
			<section class="flex flex-col gap-3">
				{#if off}
					<p class="text-[13px] text-fg-muted">
						{i18n.music_tags_panel_off()}
						<a href="/settings/music" class="text-accent underline-offset-2 hover:underline">{i18n.music_tags_panel_settings()}</a>
					</p>
				{:else}
					<p class="text-[13px] text-fg-muted">
						{tags.files.length === 1
							? i18n.music_tags_panel_match_one({ count: String(inStep), total: String(tags.files.length) })
							: i18n.music_tags_panel_match_other({ count: String(inStep), total: String(tags.files.length) })}
					</p>
				{/if}
				<div class="flex flex-wrap items-start justify-between gap-3">
					<Checkbox
						checked={tags.managed}
						label={i18n.music_tags_panel_managed()}
						description={tags.mode === "managed" ? i18n.music_tags_panel_managed_help_managed() : i18n.music_tags_panel_managed_help_import()}
						disabled={saveAlbum.isPending}
						onChange={(v: boolean) => saveAlbum.mutate({ tags_managed: v })}
					/>
					<button
						type="button"
						onclick={() => writeNow.mutate()}
						disabled={off || writeNow.isPending || tags.files.length === 0}
						class="inline-flex h-9 shrink-0 items-center gap-2 rounded-lg bg-accent px-3 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-50"
					>
						<PenLine size={15} aria-hidden="true" />
						{i18n.music_tags_panel_write()}
					</button>
				</div>
			</section>

			<section aria-labelledby="tags-album-overrides" class="flex flex-col gap-2">
				<h3 id="tags-album-overrides" class={caps}>{i18n.music_tags_panel_album_overrides()}</h3>
				<p class="text-[12.5px] text-fg-subtle">{i18n.music_tags_panel_album_overrides_help()}</p>
				<TagOverridesEditor
					value={tags.overrides}
					pending={saveAlbum.isPending}
					onSave={(o) => saveAlbum.mutate({ tag_overrides: o })}
				/>
			</section>

			{#if file}
				<section aria-labelledby="tags-file" class="flex flex-col gap-3">
					<div class="flex flex-wrap items-center gap-3">
						<h3 id="tags-file" class={caps}>{i18n.music_tags_panel_file()}</h3>
						<div class="w-80 max-w-full">
							<Select
								value={String(file.media_file_id)}
								options={fileOptions}
								ariaLabel={i18n.music_tags_panel_file()}
								onChange={(v) => (fileId = Number(v))}
								readOnlyExempt
							/>
						</div>
						<Checkbox checked={showAll} label={i18n.music_tags_panel_show_all()} onChange={(v: boolean) => (showAll = v)} />
					</div>
					<p class="font-mono text-xs text-fg-muted">
						{file.file_name} · {STATE[file.state]()}
					</p>
					{#if file.state === "failed" && file.error}
						<p class="font-mono text-xs text-status-failed">{file.error}</p>
					{/if}
					{#if file.read_error}
						<p class="text-[13px] text-status-failed">{i18n.music_tags_panel_read_error({ error: file.read_error })}</p>
					{:else if rows.length === 0}
						<p class="text-[13px] text-fg-muted">{i18n.music_tags_panel_in_step()}</p>
					{:else}
						<div class="overflow-x-auto rounded-lg border border-border">
							<table class="w-full text-left text-[12.5px]">
								<thead class="bg-bg-elevated/70">
									<tr class={caps}>
										<th scope="col" class="px-3 py-2 font-normal">{i18n.music_tags_col_tag()}</th>
										<th scope="col" class="px-3 py-2 font-normal">{i18n.music_tags_col_file()}</th>
										<th scope="col" class="px-3 py-2 font-normal">{i18n.music_tags_col_desired()}</th>
									</tr>
								</thead>
								<tbody>
									{#each rows as k (k.key)}
										<tr class="border-t border-border align-top">
											<th scope="row" class="whitespace-nowrap px-3 py-2 font-mono text-[11.5px] font-normal text-fg-muted">{k.key}</th>
											<td class={cn("px-3 py-2 break-words", k.status === "differs" ? "text-fg-subtle line-through decoration-fg-faint" : "text-fg")}>
												{values(k.current)}
											</td>
											<td class="px-3 py-2 break-words">
												{#if k.status === "kept"}
													<span class="text-fg-subtle">{i18n.music_tags_status_kept()}</span>
												{:else if k.status === "differs" && k.desired.length === 0}
													<span class="text-status-wanted">{i18n.music_tags_status_removed()}</span>
												{:else}
													<span class={k.status === "differs" ? "text-status-wanted" : "text-fg"}>{values(k.desired)}</span>
												{/if}
												{#if k.overridden}
													<span class="ml-2 rounded bg-white/[0.08] px-1.5 py-0.5 text-[10.5px] text-fg-muted">{i18n.music_tags_overridden()}</span>
												{/if}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</section>

				<section aria-labelledby="tags-track-overrides" class="flex flex-col gap-2">
					<h3 id="tags-track-overrides" class={caps}>{i18n.music_tags_panel_track_overrides()}</h3>
					<p class="text-[12.5px] text-fg-subtle">{i18n.music_tags_panel_track_overrides_help()}</p>
					{#key file.track_id}
						<TagOverridesEditor
							value={file.overrides}
							pending={saveTrack.isPending}
							onSave={(o) => saveTrack.mutate({ track: file.track_id, overrides: o })}
						/>
					{/key}
				</section>
			{/if}
		</div>
	{/if}
</Modal>
