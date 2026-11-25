<script lang="ts">
	import { CircleCheck } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { mediaOf, pendingChangesQuery, type ChangeMedia } from "@lib/metadata-changes";
	import type { MetadataChange, MetadataChangeKind } from "@lib/types";
	import { oneOf, param, setParam, syncRouteQuery } from "@lib/url-state.svelte";
	import ChangeCard from "@components/changes/ChangeCard.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Every provider change a refresh left for an admin, all media in one list,
	// newest first. The list is small and the covers elsewhere read the same
	// cache entry, so it is fetched whole and filtered here.
	const changes = pendingChangesQuery();

	type Group = "edition" | "removed" | "unlisted" | "merged";
	const GROUPS: Record<Group, MetadataChangeKind[]> = {
		edition: ["edition_removed", "edition_merged", "edition_preferred"],
		removed: ["album_removed", "artist_removed", "series_removed", "movie_removed", "book_removed"],
		unlisted: ["tracks_unlisted", "episodes_unlisted"],
		merged: ["album_merged", "artist_merged"],
	};
	const GROUP_LABELS: Record<Group, () => string> = {
		edition: i18n.changes_group_edition,
		removed: i18n.changes_group_removed,
		unlisted: i18n.changes_group_unlisted,
		merged: i18n.changes_group_merged,
	};
	const MEDIA_LABELS: Record<ChangeMedia, () => string> = {
		music: i18n.music_label,
		series: i18n.settings_series,
		movies: i18n.movies_label,
		books: i18n.books_label,
	};
	const MEDIA = Object.keys(MEDIA_LABELS) as ChangeMedia[];
	const GROUP_KEYS = Object.keys(GROUPS) as Group[];

	let media = $state<ChangeMedia | "all">("all");
	let group = $state<Group | "all">("all");
	syncRouteQuery(
		"/metadata-changes",
		(p) => {
			media = oneOf<ChangeMedia | "all">(param(p, "media"), ["all", ...MEDIA], "all");
			group = oneOf<Group | "all">(param(p, "kind"), ["all", ...GROUP_KEYS], "all");
		},
		() => {
			const p = new URLSearchParams();
			setParam(p, "media", media, "all");
			setParam(p, "kind", group, "all");
			return p;
		},
	);

	const all = $derived(changes.data?.items ?? []);
	const inMedia = (c: MetadataChange, m: ChangeMedia | "all") => m === "all" || mediaOf(c.owner.type) === m;
	const inGroup = (c: MetadataChange, g: Group | "all") => g === "all" || GROUPS[g].includes(c.kind);
	// Each row of chips counts against the other row's choice.
	const mediaCount = (m: ChangeMedia | "all") => all.filter((c) => inMedia(c, m) && inGroup(c, group)).length;
	const groupCount = (g: Group | "all") => all.filter((c) => inGroup(c, g) && inMedia(c, media)).length;
	const shown = $derived(all.filter((c) => inMedia(c, media) && inGroup(c, group)));

	const chip = (on: boolean) =>
		twMerge(
			"inline-flex min-h-11 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-3 text-[13px] transition lg:h-8 lg:min-h-0",
			on
				? "border-border-strong bg-bg-selected text-fg"
				: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg",
		);
</script>

<div class="px-4 py-6 md:px-6">
	<header class="flex flex-wrap items-baseline justify-between gap-3">
		<p class="max-w-3xl text-sm text-fg-muted">{i18n.changes_intro()}</p>
		{#if all.length > 0}
			<span class="font-mono text-xs tabular-nums text-fg-subtle">
				{(all.length === 1 ? i18n.changes_waiting_one : i18n.changes_waiting_other)({ count: all.length })}
			</span>
		{/if}
	</header>

	<nav class="mt-4 flex gap-2 overflow-x-auto pb-1" aria-label={i18n.changes_filter_media()}>
		{#each ["all", ...MEDIA] as m (m)}
			{@const n = mediaCount(m as ChangeMedia | "all")}
			<button type="button" class={chip(media === m)} aria-pressed={media === m} onclick={() => (media = m as ChangeMedia | "all")}>
				<span class="font-mono font-semibold tabular-nums text-fg">{n}</span>
				{m === "all" ? i18n.common_all() : MEDIA_LABELS[m as ChangeMedia]()}
			</button>
		{/each}
	</nav>
	<nav class="mt-2 flex gap-2 overflow-x-auto pb-1" aria-label={i18n.changes_filter_kind()}>
		{#each GROUP_KEYS as g (g)}
			{@const n = groupCount(g)}
			<button type="button" class={chip(group === g)} aria-pressed={group === g} onclick={() => (group = group === g ? "all" : g)}>
				<span class="font-mono font-semibold tabular-nums text-fg">{n}</span>
				{GROUP_LABELS[g]()}
			</button>
		{/each}
	</nav>

	<section class="mt-4 overflow-hidden rounded-lg border border-border bg-bg-elevated">
		{#if changes.isPending}
			<SkeletonList variant="divided" count={4} />
		{:else if shown.length === 0}
			<div class="flex flex-col items-center gap-2 px-6 py-12 text-center">
				<CircleCheck size={28} class="text-status-available" aria-hidden="true" />
				<p class="text-sm text-fg-muted">
					{all.length === 0 ? i18n.changes_empty() : i18n.changes_empty_filtered()}
				</p>
			</div>
		{:else}
			<ul class="divide-y divide-border">
				{#each shown as c (c.id)}
					<li><ChangeCard change={c} /></li>
				{/each}
			</ul>
		{/if}
	</section>
</div>
