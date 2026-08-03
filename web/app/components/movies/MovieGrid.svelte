<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import PosterCard from "../shared/PosterCard.svelte";
	import MovieActionsMenu from "./MovieActionsMenu.svelte";
	import type { Movie } from "../../lib/types";

	let {
		movies,
		selected,
		selectMode = false,
		onToggle,
	}: {
		movies: Movie[];
		selected: Set<number>;
		selectMode?: boolean;
		onToggle: (id: number, v: boolean) => void;
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
			toast.ok(m.monitored ? "Stopped monitoring" : "Now monitoring");
		},
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	function enrich(m: Movie) {
		const f = m.media_files?.[0];
		return {
			id: m.id,
			title: m.title,
			original_title: m.original_title,
			year: m.year,
			status: movieStatus(m),
			monitored: m.monitored,
			rating: m.rating,
			resolution: f?.parsed_resolution,
			size_text: formatBytes(f?.size, ""),
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
		>
			{#snippet kebab()}
				<MovieActionsMenu {movie} />
			{/snippet}
		</PosterCard>
	{/each}
</div>
