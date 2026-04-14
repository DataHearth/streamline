<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import PosterCard from "../shared/PosterCard.svelte";
	import MovieActionsMenu from "./MovieActionsMenu.svelte";
	import type { Movie, MediaFile } from "../../lib/types";

	let {
		movies,
	}: {
		movies: Movie[];
	} = $props();

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

	function pickPrimary(files?: MediaFile[]): MediaFile | undefined {
		if (!files || files.length === 0) return undefined;
		return [...files].sort((a, b) => b.size - a.size)[0];
	}
	function formatSize(bytes: number | undefined): string | undefined {
		if (!bytes || bytes <= 0) return undefined;
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		const mb = bytes / 1_048_576;
		return `${mb.toFixed(0)} MB`;
	}
	function enrich(m: Movie) {
		const f = pickPrimary(m.media_files);
		return {
			id: m.id,
			title: m.title,
			original_title: m.original_title,
			year: m.year,
			status: m.status,
			monitored: m.monitored,
			rating: m.rating,
			resolution: f?.parsed_resolution,
			size_text: formatSize(f?.size),
		};
	}
</script>

<div
	class="grid gap-x-4 gap-y-6 grid-cols-[repeat(auto-fill,minmax(160px,1fr))] md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] xl:grid-cols-[repeat(auto-fill,minmax(200px,240px))]"
>
	{#each movies as movie (movie.id)}
		<PosterCard movie={enrich(movie)} onMonitor={() => monitor.mutate(movie)}>
			{#snippet kebab()}
				<MovieActionsMenu {movie} />
			{/snippet}
		</PosterCard>
	{/each}
</div>
