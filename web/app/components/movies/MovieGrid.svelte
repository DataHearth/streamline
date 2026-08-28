<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import PosterCard from "../shared/PosterCard.svelte";
	import MovieActionsMenu from "./MovieActionsMenu.svelte";
	import type { Movie } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		movies,
		selected,
		selectMode = false,
		onToggle,
		onLongPress,
	}: {
		movies: Movie[];
		selected: Set<number>;
		selectMode?: boolean;
		onToggle: (id: number, v: boolean) => void;
		onLongPress?: (id: number) => void;
	} = $props();

	let selectionActive = $derived(selectMode || selected.size > 0);

	const qc = useQueryClient();
	const monitor = createMutation<Movie, Error, Movie>(() => ({
		mutationFn: (m) =>
			api<Movie>(`/movies/${m.id}`, {
				method: "PATCH",
				body: { monitored: !m.monitored },
			}),
		onSuccess: (_d, m) => {
			qc.invalidateQueries({ queryKey: ["movies"] });
			toast.ok(m.monitored ? i18n.monitor_stopped() : i18n.monitor_now_monitoring());
		},
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	function enrich(m: Movie) {
		// The list response's file rollup — media_files is detail-only.
		const f = m.file_summary;
		return {
			id: m.id,
			title: m.title,
			original_title: m.original_title,
			year: m.year,
			status: movieStatus(m),
			monitored: m.monitored,
			rating: m.rating,
			resolution: f?.resolution,
			size_text: formatBytes(f?.size_bytes, ""),
		};
	}
</script>

<div
	class="grid gap-x-4 gap-y-6 grid-cols-[repeat(auto-fill,minmax(160px,1fr))] md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,1fr))]"
>
	{#each movies as movie (movie.id)}
		<PosterCard
			movie={enrich(movie)}
			onMonitor={() => monitor.mutate(movie)}
			selected={selected.has(movie.id)}
			{selectionActive}
			onSelect={(v) => onToggle(movie.id, v)}
			onLongPress={onLongPress ? () => onLongPress(movie.id) : undefined}
		>
			{#snippet kebab()}
				<MovieActionsMenu {movie} />
			{/snippet}
		</PosterCard>
	{/each}
</div>
