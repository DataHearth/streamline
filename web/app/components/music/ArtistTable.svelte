<script lang="ts">
	import { UserRound } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import { artistNow, artistTally, monitorShort, releasesCount, type Artist } from "@lib/music-books";
	import { artistPosterUrl } from "@lib/posters";
	import Poster from "@components/shared/Poster.svelte";
	import MonitorToggle from "@components/shared/MonitorToggle.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The list view: SeriesList's table, one row per artist, with the grid's
	// letter headers as rows. Columns hide on the table's own width, as there:
	// at tablet the table has ~700px, and genre and monitoring step aside first.
	type Section = { letter: string | null; count?: number; items: Artist[] };
	let {
		sections,
		canEdit = false,
		onSearch,
		onMonitor,
		monitorPendingId = null,
		anchorClass = "",
	}: {
		sections: Section[];
		canEdit?: boolean;
		onSearch: (a: Artist) => void;
		onMonitor: (a: Artist) => void;
		monitorPendingId?: number | null;
		// The scroll margin a letter jump lands with, under the sticky bars.
		anchorClass?: string;
	} = $props();

	// Same precedence as the card's pill: a refused import, what is missing,
	// what is on its way, then whether the followed releases are all there.
	function dotOf(a: Artist) {
		const t = artistTally(a);
		if (t.downloading) return "downloading";
		if (artistNow(a).failed) return "failed";
		if (t.wanted) return "wanted";
		return t.released ? "available" : "missing";
	}
</script>

<div class="@container overflow-x-auto overflow-y-hidden rounded-lg border border-border bg-bg-elevated/70 backdrop-blur-md">
	<table class="w-full text-sm">
		<thead class="bg-bg-elevated/95 text-[10px] uppercase tracking-[0.12em] text-fg-subtle">
			<tr class="border-b border-border">
				<th scope="col" class="w-14 px-3 py-2.5" aria-hidden="true"></th>
				<th scope="col" class="px-3 py-2.5 text-left font-medium">{i18n.music_artist()}</th>
				<th scope="col" class="hidden w-36 px-3 py-2.5 text-left font-medium @3xl:table-cell">{i18n.music_fact_genre()}</th>
				<th scope="col" class="hidden w-32 px-3 py-2.5 text-left font-medium @4xl:table-cell">{i18n.music_monitoring()}</th>
				<th scope="col" class="w-56 px-3 py-2.5 text-left font-medium">{i18n.music_fact_releases()}</th>
				<th scope="col" class="hidden w-24 px-3 py-2.5 text-right font-medium @2xl:table-cell">{i18n.common_size()}</th>
				<th scope="col" class="w-24 px-3 py-2.5" aria-hidden="true"></th>
			</tr>
		</thead>
		<tbody>
			{#each sections as s}
				{#if s.letter}
					<tr data-letter={s.letter} class={anchorClass}>
						<td colspan="7" class="border-b border-border bg-surface px-3 py-1.5">
							<span class="font-mono text-[11px] font-semibold text-fg">{s.letter}</span>
							{#if s.count != null}
								<span class="ml-2 font-mono text-[10.5px] text-fg-subtle">{s.count.toLocaleString()}</span>
							{/if}
						</td>
					</tr>
				{/if}
				{#each s.items as a (a.id)}
					{@const t = artistTally(a)}
					{@const failed = artistNow(a).failed}
					<tr class="group border-b border-border transition last:border-b-0 hover:bg-surface">
						<td class="px-3 py-2">
							<!-- The size sits on the inner disc: on the link, tokens.css's
							     touch floor would stretch it into an oval below lg. -->
							<!-- biome-ignore lint/a11y/useAnchorContent: a pointer-only twin of the named link beside it, out of the tab order -->
							<a href="/music/{a.id}" tabindex="-1" aria-hidden="true" class="block w-fit">
								<span class="relative block h-9 w-9 overflow-hidden rounded-full bg-bg-card">
									<span class="absolute inset-0 grid place-items-center text-fg-faint">
										<UserRound class="h-4 w-4" aria-hidden="true" />
									</span>
									<Poster src={artistPosterUrl(a.id)} alt="" class="relative h-full w-full object-cover" />
								</span>
							</a>
						</td>
						<td class="max-w-0 px-3 py-2">
							<a
								href="/music/{a.id}"
								class="block truncate font-medium text-fg transition hover:text-accent-text focus:outline-none focus-visible:underline"
							>
								{a.name}
							</a>
						</td>
						<td class="hidden max-w-0 truncate px-3 py-2 font-mono text-xs text-fg-muted @3xl:table-cell">
							{a.genre || "—"}
						</td>
						<td
							class={cn(
								"hidden whitespace-nowrap px-3 py-2 text-[12.5px] @4xl:table-cell",
								a.monitor === "none" ? "text-fg-subtle" : "text-fg-muted",
							)}
						>
							{monitorShort(a.monitor)}
						</td>
						<td class="whitespace-nowrap px-3 py-2 font-mono text-xs tabular text-fg">
							<span class="inline-flex items-center gap-2">
								<span class="h-[7px] w-[7px] shrink-0 rounded-full" style:background-color="var(--status-{dotOf(a)})"></span>
								{#if t.released}
									<span>{t.have}<span class="text-fg-muted">/{t.released}</span></span>
								{:else}
									<span class="text-fg-muted">{releasesCount(t.out)}</span>
								{/if}
								{#if t.wanted}
									<span class="text-status-wanted">· {i18n.music_n_wanted({ count: String(t.wanted) })}</span>
								{/if}
								{#if t.downloading}
									<span class="text-status-downloading">· {i18n.music_n_downloading({ count: String(t.downloading) })}</span>
								{/if}
								{#if failed}
									<span class="lowercase text-status-failed">· {i18n.status_failed()}</span>
								{/if}
							</span>
						</td>
						<td class="hidden whitespace-nowrap px-3 py-2 text-right font-mono text-xs tabular text-fg-muted @2xl:table-cell">
							{a.size > 0 ? formatBytes(a.size) : "—"}
						</td>
						<td class="px-3 py-2">
							{#if canEdit}
								<div class="flex items-center justify-end gap-1">
									<MonitorToggle
										monitored={a.monitor !== "none"}
										onToggle={() => onMonitor(a)}
										pending={monitorPendingId === a.id}
										variant="ghost"
										size="row"
									/>
									<LibraryActions
										base={`/music/artists/${a.id}`}
										title={a.name}
										media="music"
										queryKey="music"
										backHref="/music"
										hasFiles={a.size > 0}
										profile={a.quality_profile}
										searchLabel={t.wanted > 0 ? i18n.music_search_missing() : undefined}
										onSearch={() => onSearch(a)}
										removeBody={i18n.music_remove_body()}
										filesLabel={i18n.music_delete_files_label()}
										variant="row"
									/>
								</div>
							{/if}
						</td>
					</tr>
				{/each}
			{/each}
		</tbody>
	</table>
</div>
