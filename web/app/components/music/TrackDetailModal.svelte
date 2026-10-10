<script lang="ts">
	import { Search, Trash2 } from "@lucide/svelte";
	import Modal from "@components/modals/Modal.svelte";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import { multiDisc, personHref, releaseYear, trackTime, type Person, type Release, type Track } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// EpisodeDetailModal's counterpart: what is known about one track, and the
	// row's two actions again for anyone who opened it to act.
	let {
		track,
		release,
		qualityNote = "",
		canEdit = false,
		onSearch,
		onDeleteFile,
		onClose,
	}: {
		track: Track | null;
		release?: Release;
		qualityNote?: string;
		canEdit?: boolean;
		onSearch?: (t: Track) => void;
		onDeleteFile?: (t: Track) => void;
		onClose: () => void;
	} = $props();

	type Row = { k: string; v?: string; people?: Person[] };
	let rows = $derived.by(() => {
		if (!track) return [] as Row[];
		const r: Row[] = [];
		if (release) r.push({ k: i18n.music_fact_release(), v: [release.title, releaseYear(release)].filter(Boolean).join(" · ") });
		r.push({ k: i18n.music_fact_track(), v: release ? i18n.music_track_n_of({ n: multiDisc(release) ? `${track.disc}.${track.number}` : String(track.number), total: String(release.track_count) }) : String(track.number) });
		r.push({ k: i18n.music_fact_duration(), v: trackTime(track.duration) });
		if (track.featuring?.length) r.push({ k: i18n.music_fact_featuring(), people: track.featuring });
		if (track.writers?.length) r.push({ k: i18n.music_fact_written_by(), people: track.writers });
		if (track.has_file && qualityNote) r.push({ k: i18n.music_fact_quality(), v: qualityNote });
		return r;
	});
	function act(fn?: (t: Track) => void) {
		const t = track;
		onClose();
		if (t && fn) fn(t);
	}
</script>

<Modal open={!!track} title={track?.title ?? ""} size="md" {onClose}>
	{#if track}
		<dl class="divide-y divide-border rounded-lg border border-border bg-bg-card/50 px-4 text-[13px]">
			<div class="flex items-center justify-between gap-4 py-2.5">
				<dt class="text-fg-subtle">{i18n.common_status()}</dt>
				<dd>
					{#if track.has_file}
						<LabelPill token="available" label={i18n.music_track_on_disk()} variant="translucent" />
					{:else}
						<LabelPill token="wanted" label={i18n.music_track_missing()} variant="translucent" />
					{/if}
				</dd>
			</div>
			{#each rows as row (row.k)}
				<div class="flex items-center justify-between gap-4 py-2.5">
					<dt class="shrink-0 text-fg-subtle">{row.k}</dt>
					{#if row.people}
						<!-- A guest who is in the library opens their artist page; the
						     dialog closes first, or it would sit over the next page. -->
						<dd class="min-w-0 text-right text-fg [text-wrap:pretty]">
							{#each row.people as p, i (p.name)}{#if i > 0}<span class="text-fg-faint">, </span>{/if}{#if personHref(p)}<a href={personHref(p)} onclick={onClose} class="text-accent-text transition hover:text-accent">{p.name}</a>{:else}{p.name}{/if}{/each}
						</dd>
					{:else}
						<dd class="min-w-0 truncate text-right text-fg" title={row.v}>{row.v}</dd>
					{/if}
				</div>
			{/each}
		</dl>
	{/if}
	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-sm font-medium text-fg hover:border-border-strong"
		>
			{i18n.common_close()}
		</button>
		{#if canEdit && track?.has_file && onDeleteFile}
			<button
				type="button"
				onclick={() => act(onDeleteFile)}
				class="inline-flex items-center gap-1.5 rounded-md border border-status-failed/40 px-3 py-1.5 text-sm font-medium text-status-failed hover:bg-status-failed/10"
			>
				<Trash2 size={14} aria-hidden="true" />
				{i18n.action_delete_file()}
			</button>
		{/if}
		{#if canEdit && onSearch}
			<button
				type="button"
				onclick={() => act(onSearch)}
				class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover"
			>
				<Search size={14} aria-hidden="true" />
				{i18n.action_manual_search()}
			</button>
		{/if}
	{/snippet}
</Modal>
