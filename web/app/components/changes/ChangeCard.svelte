<script lang="ts">
	import { onMount } from "svelte";
	import { goto } from "@roxi/routify";
	import { BookOpen, CircleX, Disc3, Film, GitMerge, Music, TriangleAlert, Tv } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { api } from "@lib/api";
	import { BOOKS, createAction, deleteFailed, MOVIES, MUSIC, SERIES, updateFailed } from "@lib/actions";
	import { formatRelative } from "@lib/dates";
	import { followChange, isItemRemoved, keepChange, ownerHref } from "@lib/metadata-changes";
	import { NOUN_FILE } from "@lib/nouns";
	import { albumPosterUrl, artistPosterUrl, bookPosterUrl, posterUrl, tvPosterUrl } from "@lib/posters";
	import type { MetadataChange, MetadataChangeEdition } from "@lib/types";
	import Button from "@components/shared/Button.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import Poster from "@components/shared/Poster.svelte";
	import ReidentifyDialog from "@components/shared/ReidentifyDialog.svelte";
	import AlbumEditionPicker from "@components/music/AlbumEditionPicker.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One provider change waiting on an admin, as a row of the Metadata
	// changes list or as the banner on its item's own page. Follow and keep
	// are answered here; finding a removed title again and removing it run
	// here too, the editions through the album's own picker, or the page's.
	let {
		change: c,
		variant = "row",
		onPickEdition,
	}: {
		change: MetadataChange;
		variant?: "row" | "banner";
		onPickEdition?: () => void;
	} = $props();

	let navigate = $state<(p: string) => void>(() => {});
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	const provider = $derived(
		c.owner.type === "tv_show"
			? "TVDB"
			: c.owner.type === "movie"
				? "TMDB"
				: c.owner.type === "book"
					? "Hardcover"
					: "MusicBrainz",
	);
	const removed = $derived(isItemRemoved(c.kind));
	const merged = $derived(c.kind === "edition_merged" || c.kind === "album_merged" || c.kind === "artist_merged");
	const editionKind = $derived(
		c.kind === "edition_removed" || c.kind === "edition_merged" || c.kind === "edition_preferred",
	);
	const canFindAgain = $derived(c.kind === "movie_removed" || c.kind === "series_removed");
	const removeBase = $derived(
		c.kind === "movie_removed"
			? `/movies/${c.owner.id}`
			: c.kind === "series_removed"
				? `/series/${c.owner.id}`
				: c.kind === "artist_removed"
					? `/music/artists/${c.owner.id}`
					: c.kind === "book_removed"
						? `/books/${c.owner.id}`
						: null,
	);
	const sectionHref = $derived(
		c.owner.type === "movie"
			? "/movies"
			: c.owner.type === "tv_show"
				? "/series"
				: c.owner.type === "book"
					? "/books"
					: "/music",
	);
	const refreshKeys = $derived(
		c.owner.type === "movie" ? MOVIES : c.owner.type === "tv_show" ? SERIES : c.owner.type === "book" ? BOOKS : MUSIC,
	);
	const files = $derived(c.details.files ?? 0);

	const cover = $derived(
		c.owner.type === "album"
			? albumPosterUrl(c.owner.id)
			: c.owner.type === "artist"
				? artistPosterUrl(c.owner.id)
				: c.owner.type === "tv_show"
					? tvPosterUrl(c.owner.id)
					: c.owner.type === "movie"
						? posterUrl({ id: c.owner.id })
						: bookPosterUrl(c.owner.id),
	);
	const square = $derived(c.owner.type === "album" || c.owner.type === "artist");
	const TypeIcon = $derived(
		c.owner.type === "album"
			? Disc3
			: c.owner.type === "artist"
				? Music
				: c.owner.type === "tv_show"
					? Tv
					: c.owner.type === "movie"
						? Film
						: BookOpen,
	);
	const typeLabel = $derived(
		{
			album: i18n.change_type_album,
			artist: i18n.change_type_artist,
			tv_show: i18n.change_type_series,
			movie: i18n.change_type_movie,
			book: i18n.change_type_book,
		}[c.owner.type](),
	);

	const kindLabel = $derived(
		{
			edition_removed: i18n.change_kind_edition_removed,
			edition_merged: i18n.change_kind_edition_merged,
			edition_preferred: i18n.change_kind_edition_preferred,
			tracks_unlisted: i18n.change_kind_tracks_unlisted,
			album_removed: i18n.change_kind_album_removed,
			album_merged: i18n.change_kind_album_merged,
			artist_removed: i18n.change_kind_artist_removed,
			artist_merged: i18n.change_kind_artist_merged,
			series_removed: i18n.change_kind_series_removed,
			episodes_unlisted: i18n.change_kind_episodes_unlisted,
			movie_removed: i18n.change_kind_movie_removed,
			book_removed: i18n.change_kind_book_removed,
		}[c.kind](),
	);

	const why = $derived.by(() => {
		const target = c.details.target?.title ?? "";
		switch (c.kind) {
			case "edition_preferred":
				return i18n.change_why_edition_preferred();
			case "edition_merged":
				return i18n.change_why_edition_merged();
			case "edition_removed":
				return c.owner.type === "book"
					? i18n.change_why_book_edition_removed()
					: i18n.change_why_edition_removed();
			case "tracks_unlisted":
				return i18n.change_why_tracks_unlisted();
			case "album_removed":
				return i18n.change_why_album_removed();
			case "album_merged":
				return c.details.target?.id
					? i18n.change_why_album_merged_library({ target })
					: i18n.change_why_album_merged({ target });
			case "artist_removed":
				return i18n.change_why_artist_removed();
			case "artist_merged":
				return c.details.target?.id
					? i18n.change_why_artist_merged_library({ target })
					: i18n.change_why_artist_merged({ target });
			case "series_removed":
				return i18n.change_why_series_removed();
			case "episodes_unlisted":
				return i18n.change_why_episodes_unlisted();
			case "movie_removed":
				return i18n.change_why_movie_removed();
			case "book_removed":
				return i18n.change_why_book_removed();
		}
	});

	function summary(e: MetadataChangeEdition | undefined): string {
		if (!e) return "";
		const parts = [
			e.format,
			e.publisher,
			e.year ? String(e.year) : e.date,
			e.country,
			...(e.formats ?? []),
			e.language,
			e.tracks
				? (e.tracks === 1 ? i18n.change_tracks_one : i18n.change_tracks_other)({ count: e.tracks })
				: undefined,
		];
		return parts.filter(Boolean).join(" · ");
	}
	const currents = $derived(
		c.details.current === undefined
			? []
			: Array.isArray(c.details.current)
				? c.details.current
				: [c.details.current],
	);

	const follow = createAction(() => ({
		run: () => followChange(c.id),
		refresh: [["metadata-changes"], ...refreshKeys],
		failed: updateFailed,
		succeeded: () => i18n.change_followed({ provider }),
	}));
	const keep = createAction(() => ({
		run: () => keepChange(c.id),
		refresh: [["metadata-changes"]],
		failed: updateFailed,
		succeeded: () => i18n.change_kept(),
	}));

	let deleteOpen = $state(false);
	let reidentifyOpen = $state(false);
	let pickOpen = $state(false);
	const remove = createAction<boolean>(() => ({
		run: (withFiles) => api(`${removeBase}?delete_files=${withFiles}`, { method: "DELETE" }),
		refresh: [["metadata-changes"], ...refreshKeys],
		failed: deleteFailed,
		succeeded: () => i18n.library_removed({ title: c.owner.title }),
		after: () => {
			deleteOpen = false;
			if (variant === "banner") navigate(sectionHref);
		},
	}));

	function pickEdition() {
		if (onPickEdition) onPickEdition();
		else if (c.owner.type === "album" && variant === "banner") pickOpen = !pickOpen;
		else navigate(ownerHref(c));
	}

	const pillTone = $derived(
		removed
			? "bg-status-failed/15 text-status-failed"
			: merged
				? "bg-accent-soft text-accent-text"
				: "bg-status-wanted/15 text-status-wanted",
	);
	const busy = $derived(follow.isPending || keep.isPending);
</script>

{#snippet pill()}
	<span
		class={twMerge(
			"inline-flex items-center gap-1.5 rounded px-2 py-0.5 text-[11px] font-semibold tracking-[0.02em]",
			pillTone,
		)}
	>
		{#if removed}
			<CircleX size={12} aria-hidden="true" />
		{:else if merged}
			<GitMerge size={12} aria-hidden="true" />
		{:else}
			<TriangleAlert size={12} aria-hidden="true" />
		{/if}
		{kindLabel}
	</span>
{/snippet}

{#snippet body()}
	<p class="mt-1.5 text-[13px] leading-relaxed text-fg-muted">{why}</p>
	{#if editionKind && (currents.length > 0 || c.details.upstream)}
		<div class="mt-2.5 grid gap-2 sm:grid-cols-2">
			{#each currents as cur, i (i)}
				<div class="rounded-md border border-border bg-surface px-2.5 py-2">
					<span class="eyebrow block text-[10px] text-fg-subtle">{i18n.change_you_follow()}</span>
					<span class="text-[13px] text-fg">{summary(cur)}</span>
				</div>
			{/each}
			{#if c.details.upstream}
				<div class="rounded-md border border-accent-line bg-accent-soft px-2.5 py-2">
					<span class="eyebrow block text-[10px] text-fg-subtle">
						{c.kind === "edition_preferred"
							? i18n.change_provider_prefers({ provider })
							: i18n.change_provider_has({ provider })}
					</span>
					<span class="text-[13px] text-fg">{summary(c.details.upstream)}</span>
				</div>
			{/if}
		</div>
	{/if}
	{#if c.details.items?.length}
		<ul class="mt-2.5 divide-y divide-border rounded-md border border-status-wanted/30 bg-status-wanted/[0.06]">
			{#each c.details.items as item (item.id)}
				<li class="flex items-baseline gap-3 px-2.5 py-1.5 text-[13px]">
					<span class="shrink-0 font-mono text-[11.5px] text-fg-subtle">{item.label}</span>
					<span class="min-w-0 truncate text-fg">{item.title}</span>
				</li>
			{/each}
		</ul>
	{/if}
	<div class="mt-3 flex flex-wrap gap-2">
		{#if !removed}
			<Button size="sm" disabled={busy} onclick={() => follow.mutate()}>
				<span class="max-md:hidden">{i18n.change_follow({ provider })}</span>
				<span class="md:hidden">{i18n.change_follow_short()}</span>
			</Button>
		{/if}
		{#if canFindAgain}
			<Button size="sm" onclick={() => (reidentifyOpen = true)}>{i18n.change_find_again()}</Button>
		{/if}
		<Button size="sm" variant="secondary" disabled={busy} onclick={() => keep.mutate()}>
			<span class="max-md:hidden">{i18n.change_keep()}</span>
			<span class="md:hidden">{i18n.change_keep_short()}</span>
		</Button>
		{#if editionKind}
			<Button size="sm" variant="ghost" aria-expanded={pickOpen} onclick={pickEdition}>
				<span class="max-md:hidden">{i18n.change_pick_edition()}</span>
				<span class="md:hidden">{i18n.change_pick_short()}</span>
			</Button>
		{/if}
		{#if removeBase}
			<Button size="sm" variant="ghost" onclick={() => (deleteOpen = true)}>{i18n.change_remove()}</Button>
		{/if}
	</div>
	{#if pickOpen && c.owner.type === "album"}
		<div class="mt-3">
			<AlbumEditionPicker albumId={c.owner.id} />
		</div>
	{/if}
{/snippet}

{#if variant === "banner"}
	<div
		role="status"
		class={twMerge(
			"rounded-md border px-3.5 py-3",
			removed
				? "border-status-failed/40 bg-status-failed/[0.08]"
				: "border-status-wanted/40 bg-status-wanted/[0.08]",
		)}
	>
		{@render pill()}
		{@render body()}
	</div>
{:else}
	<article class="grid grid-cols-[44px_minmax(0,1fr)] gap-3 px-4 py-3.5 md:grid-cols-[44px_minmax(0,1fr)_auto]">
		<a
			href={ownerHref(c)}
			class={twMerge(
				"relative block w-11 overflow-hidden rounded bg-bg-card",
				square ? "aspect-square" : "aspect-[2/3]",
			)}
			aria-label={c.owner.title}
		>
			<Poster src={cover} alt="" class="h-full w-full object-cover" />
		</a>
		<div class="min-w-0">
			<a href={ownerHref(c)} class="font-semibold text-fg hover:underline">{c.owner.title}</a>
			<span class="text-xs text-fg-subtle">
				{[c.owner.artist_name, c.owner.year ? String(c.owner.year) : undefined, files ? NOUN_FILE.count(files) : undefined]
					.filter(Boolean)
					.map((p) => ` · ${p}`)
					.join("")}
			</span>
			<div class="mt-0.5 flex items-center gap-1 text-xs text-fg-subtle">
				<TypeIcon size={12} aria-hidden="true" />
				{typeLabel}
			</div>
			<div class="mt-1.5">{@render pill()}</div>
			{@render body()}
		</div>
		<span class="hidden font-mono text-xs text-fg-subtle md:block">{formatRelative(c.detected_at)}</span>
	</article>
{/if}

{#if canFindAgain}
	<ReidentifyDialog
		open={reidentifyOpen}
		kind={c.owner.type === "movie" ? "movie" : "series"}
		id={c.owner.id}
		currentTitle={c.owner.title}
		onClose={() => (reidentifyOpen = false)}
	/>
{/if}
{#if removeBase}
	<DeleteTitleDialog
		open={deleteOpen}
		title={i18n.series_remove_title({ title: c.owner.title })}
		body={c.owner.type === "movie"
			? i18n.movie_remove_body()
			: c.owner.type === "tv_show"
				? i18n.series_remove_body()
				: c.owner.type === "artist"
					? i18n.music_remove_body()
					: i18n.books_remove_body()}
		filesLabel={i18n.bulk_delete_files_label({ items: NOUN_FILE.items })}
		filesNote={i18n.common_cannot_undo()}
		canDeleteFiles={true}
		pending={remove.isPending}
		onClose={() => (deleteOpen = false)}
		onConfirm={(withFiles) => remove.mutate(withFiles)}
	/>
{/if}
