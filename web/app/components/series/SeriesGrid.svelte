<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { tvPosterUrl } from "../../lib/posters";
	import PosterCard from "../shared/PosterCard.svelte";
	import SeriesActionsMenu from "./SeriesActionsMenu.svelte";
	import type { StatusKind } from "../shared/StatusPill.svelte";
	import type { TVShow } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		series,
		selected,
		selectMode = false,
		onToggle,
		onLongPress,
	}: {
		series: TVShow[];
		selected: Set<number>;
		selectMode?: boolean;
		onToggle: (id: number, v: boolean) => void;
		onLongPress?: (id: number) => void;
	} = $props();

	let selectionActive = $derived(selectMode || selected.size > 0);

	const qc = useQueryClient();
	const monitor = createMutation<TVShow, Error, TVShow>(() => ({
		mutationFn: (s) =>
			api<TVShow>(`/series/${s.id}`, {
				method: "PATCH",
				body: { monitored: !s.monitored },
			}),
		onSuccess: (_d, s) => {
			qc.invalidateQueries({ queryKey: ["series"] });
			toast.ok(s.monitored ? i18n.monitor_stopped() : i18n.monitor_now_monitoring());
		},
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	function cardStatus(s: TVShow): StatusKind {
		// A grab in flight outranks the gaps behind it: the card's next change of
		// state is the download landing, not the episode being wanted — and an
		// in-flight episode is counted wanted until its file exists.
		if ((s.downloading_episodes ?? 0) > 0) return "downloading";
		// Past the grab, being written into the library.
		if ((s.importing_episodes ?? 0) > 0) return "importing";
		if ((s.wanted_episodes ?? 0) > 0) return "wanted";
		// Unmonitored shows report zero wanted episodes, so "nothing wanted" alone
		// would badge an empty library entry as available.
		return (s.have_episodes ?? 0) > 0 ? "available" : "missing";
	}

	function episodeText(s: TVShow): string | undefined {
		const seasons = s.total_seasons ?? 0;
		const seasonText =
			seasons > 0 ? `${seasons} season${seasons === 1 ? "" : "s"}` : undefined;
		// A show the library follows no episode of still has seasons, and that
		// is the only size the card can report for it.
		if (!s.total_episodes) return seasonText;
		const eps = `${s.have_episodes ?? 0}/${s.total_episodes} eps`;
		return seasonText ? `${seasonText} · ${eps}` : eps;
	}

	// While something is in flight the card names what is coming rather than
	// what it holds — the episode, the season pack, or the whole series. The
	// have/total count is the one thing about to change.
	const pad = (n: number) => String(n).padStart(2, "0");
	function downloadText(s: TVShow): string {
		if (
			s.downloading_scope === "episode" &&
			s.downloading_season != null &&
			s.downloading_episode != null
		)
			return `S${pad(s.downloading_season)}E${pad(s.downloading_episode)}`;
		if (s.downloading_scope === "season" && s.downloading_season != null)
			return i18n.series_dl_season_pack({ season: pad(s.downloading_season) });
		if (s.downloading_scope === "series") return i18n.series_dl_full_series();
		// Scope unknown — the count still says how much is in flight.
		return `${s.downloading_episodes || s.importing_episodes || 0} eps`;
	}

	function enrich(s: TVShow) {
		const status = cardStatus(s);
		const downloading = status === "downloading";
		const inFlight = downloading || status === "importing";
		return {
			id: s.id,
			title: s.title,
			original_title: s.original_title,
			year: s.year,
			releaseDate: s.first_aired,
			status,
			monitored: s.monitored,
			rating: s.rating ?? undefined,
			// Undefined progress draws the indeterminate bar, which is the honest
			// reading when the server has not sent one — and an import has no
			// percentage to send.
			progress: downloading ? s.download_progress : undefined,
			size_text: inFlight ? downloadText(s) : episodeText(s),
		};
	}
</script>

<div
	class="grid gap-x-4 gap-y-6 grid-cols-[repeat(auto-fill,minmax(160px,1fr))] md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,1fr))]"
>
	{#each series as show (show.id)}
		<PosterCard
			movie={enrich(show)}
			href={`/series/${show.id}`}
			posterSrc={tvPosterUrl(show.id)}
			onMonitor={() => monitor.mutate(show)}
			selected={selected.has(show.id)}
			{selectionActive}
			onSelect={(v) => onToggle(show.id, v)}
			onLongPress={onLongPress ? () => onLongPress(show.id) : undefined}
		>
			{#snippet kebab()}
				<SeriesActionsMenu {show} />
			{/snippet}
		</PosterCard>
	{/each}
</div>
