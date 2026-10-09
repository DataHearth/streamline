<script lang="ts">
	import { Info, Search, Trash2 } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import KebabMenu, { type KebabItem } from "@components/shared/KebabMenu.svelte";
	import TrackDetailModal from "./TrackDetailModal.svelte";
	import { peopleList, trackTime, type Release, type Track } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A plain numbered tracklist. Quality is stated once in the header rather
	// than on every row; a track carries a mark only when it is missing. Each row
	// has the episode table's actions — details, search, delete file — inline
	// from lg, folded into one kebab below it, where three 44px targets would
	// squeeze the title out. The title itself opens the details.
	let {
		tracks,
		release,
		qualityNote = "",
		markMissing = false,
		compact = false,
		canEdit = false,
		onSearch,
		onDeleteFile,
	}: {
		tracks: Track[];
		release?: Release;
		qualityNote?: string;
		markMissing?: boolean;
		compact?: boolean;
		canEdit?: boolean;
		onSearch?: (t: Track) => void;
		onDeleteFile?: (t: Track) => void;
	} = $props();

	let detail = $state<Track | null>(null);
	let canSearch = $derived(canEdit && !!onSearch);
	const canDelete = (t: Track) => canEdit && !!onDeleteFile && t.has_file;
	const items = (t: Track): KebabItem[] => [
		{ key: "info", label: i18n.common_details(), icon: Info, onSelect: () => (detail = t) },
		...(canSearch ? [{ key: "search", label: i18n.action_manual_search(), icon: Search, onSelect: () => onSearch?.(t) } satisfies KebabItem] : []),
		...(canDelete(t)
			? [{ key: "delete", label: i18n.action_delete_file(), icon: Trash2, danger: true, dividerBefore: true, onSelect: () => onDeleteFile?.(t) } satisfies KebabItem]
			: []),
	];
	const iconBtn =
		"grid h-7 w-7 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg focus-visible:ring-2 focus-visible:ring-accent-ring";
</script>

<div class="overflow-hidden rounded-lg border border-border bg-bg-elevated/70">
	{#if !compact}
		<div
			class="flex items-center justify-between gap-3 border-b border-border px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<span>{i18n.music_tracklist()}</span>
			{#if qualityNote}
				<span class="truncate">{qualityNote}</span>
			{/if}
		</div>
	{/if}
	<ol>
		{#each tracks as t (t.id)}
			<li
				class={cn(
					"flex min-h-11 items-center border-b border-border/60 last:border-b-0",
					compact ? "gap-3 pl-3 pr-0.5" : "gap-4 pl-4 pr-1 lg:min-h-10 lg:pr-3",
				)}
			>
				<span class={cn("shrink-0 text-right font-mono text-fg-faint", compact ? "w-5 text-[11px]" : "w-6 text-xs")}>
					{t.number}
				</span>
				<button
					type="button"
					onclick={() => (detail = t)}
					class={cn(
						"min-w-0 flex-1 truncate py-2.5 text-left transition hover:text-accent-text",
						compact ? "text-[13.5px]" : "text-[14px]",
						markMissing && !t.has_file ? "text-fg-muted" : "text-fg",
					)}
					title={t.title}
				>
					{t.title}{#if t.featuring?.length && !compact}<span class="text-fg-subtle"> {i18n.music_featuring({ name: peopleList(t.featuring) })}</span>{/if}
				</button>
				{#if t.bonus && !compact}
					<span class="shrink-0 rounded-md border border-border bg-surface px-1.5 py-0.5 text-[11px] font-medium text-fg-muted">
						{i18n.music_bonus()}
					</span>
				{/if}
				{#if markMissing && !t.has_file}
					<LabelPill token="wanted" label={i18n.music_track_missing()} variant="translucent" />
				{/if}
				<span class={cn("shrink-0 text-right font-mono text-fg-muted", compact ? "text-[11px]" : "w-10 text-xs")}>
					{trackTime(t.duration)}
				</span>
				<div class="hidden w-[88px] shrink-0 items-center justify-end gap-0.5 lg:flex">
					<button type="button" onclick={() => (detail = t)} aria-label={i18n.common_details()} title={i18n.common_details()} class={iconBtn}>
						<Info size={14} aria-hidden="true" />
					</button>
					{#if canSearch}
						<button
							type="button"
							onclick={() => onSearch?.(t)}
							aria-label={i18n.music_track_search_for({ title: t.title })}
							title={i18n.action_manual_search()}
							class={iconBtn}
						>
							<Search size={14} aria-hidden="true" />
						</button>
					{/if}
					{#if canDelete(t)}
						<button
							type="button"
							onclick={() => onDeleteFile?.(t)}
							aria-label={i18n.music_track_delete_for({ title: t.title })}
							title={i18n.action_delete_file()}
							class={cn(iconBtn, "hover:bg-status-failed/10 hover:text-status-failed")}
						>
							<Trash2 size={14} aria-hidden="true" />
						</button>
					{/if}
				</div>
				<div class="shrink-0 lg:hidden">
					<KebabMenu items={items(t)} variant="row" />
				</div>
			</li>
		{/each}
	</ol>
</div>

<TrackDetailModal
	track={detail}
	{release}
	{qualityNote}
	{canEdit}
	onSearch={canSearch ? onSearch : undefined}
	{onDeleteFile}
	onClose={() => (detail = null)}
/>
