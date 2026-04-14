<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { tvPosterUrl } from "../../lib/posters";
	import PosterCard from "../shared/PosterCard.svelte";
	import SeriesActionsMenu from "./SeriesActionsMenu.svelte";
	import type { StatusKind } from "../shared/StatusPill.svelte";
	import type { TVShow } from "../../lib/types";

	let { series }: { series: TVShow[] } = $props();

	const qc = useQueryClient();
	const monitor = createMutation<TVShow, Error, TVShow>(() => ({
		mutationFn: (s) =>
			api<TVShow>(`/series/${s.id}`, {
				method: "PATCH",
				body: { monitored: !s.monitored },
			}),
		onSuccess: (_d, s) => {
			qc.invalidateQueries({ queryKey: ["series"] });
			toast.ok(s.monitored ? "Stopped monitoring" : "Now monitoring");
		},
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	function cardStatus(s: TVShow): StatusKind {
		return (s.wanted_episodes ?? 0) > 0 ? "wanted" : "available";
	}

	function episodeText(s: TVShow): string | undefined {
		if (!s.total_episodes) return undefined;
		return `${s.have_episodes ?? 0}/${s.total_episodes} eps`;
	}

	function enrich(s: TVShow) {
		return {
			id: s.id,
			title: s.title,
			original_title: s.original_title,
			year: s.year,
			status: cardStatus(s),
			monitored: s.monitored,
			rating: s.rating ?? undefined,
			size_text: episodeText(s),
		};
	}
</script>

<div
	class="grid gap-x-4 gap-y-6 grid-cols-[repeat(auto-fill,minmax(160px,1fr))] md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,240px))]"
>
	{#each series as show (show.id)}
		<PosterCard
			movie={enrich(show)}
			href={`/series/${show.id}`}
			posterSrc={tvPosterUrl(show.id)}
			onMonitor={() => monitor.mutate(show)}
		>
			{#snippet kebab()}
				<SeriesActionsMenu {show} />
			{/snippet}
		</PosterCard>
	{/each}
</div>
