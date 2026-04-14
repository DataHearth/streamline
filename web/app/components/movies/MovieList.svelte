<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Film, ChevronUp, ChevronDown, Bookmark } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import Poster from "./Poster.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import MovieActionsMenu from "./MovieActionsMenu.svelte";
	import type { Movie, MediaFile } from "../../lib/types";

	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";

	let {
		movies,
		sort,
		order,
		onSortChange,
	}: {
		movies: Movie[];
		sort: SortKey;
		order: SortOrder;
		onSortChange: (s: SortKey, o: SortOrder) => void;
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

	function totalSize(files: MediaFile[] | undefined): string {
		if (!files || files.length === 0) return "—";
		const bytes = files.reduce((s, f) => s + f.size, 0);
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		const mb = bytes / 1_048_576;
		return `${mb.toFixed(0)} MB`;
	}

	function quality(files: MediaFile[] | undefined): string {
		if (!files || files.length === 0) return "—";
		const primary = [...files].sort((a, b) => b.size - a.size)[0];
		const parts = [primary.parsed_resolution, primary.parsed_codec].filter(
			(v): v is string => Boolean(v),
		);
		return parts.length > 0 ? parts.join(" · ") : "—";
	}

	function ariaSort(key: SortKey): "ascending" | "descending" | "none" {
		if (sort !== key) return "none";
		return order === "asc" ? "ascending" : "descending";
	}

	function toggle(key: SortKey) {
		if (sort === key) {
			onSortChange(key, order === "asc" ? "desc" : "asc");
		} else {
			onSortChange(key, key === "year" ? "desc" : "asc");
		}
	}
</script>

<div
	class="overflow-hidden rounded-lg border border-border bg-bg-elevated/70 backdrop-blur-md"
>
	<table class="w-full text-sm">
		<thead
			class="bg-bg-elevated/95 text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<tr class="border-b border-border">
				<th scope="col" class="w-12 px-3 py-2.5" aria-hidden="true"></th>
				<th
					scope="col"
					aria-sort={ariaSort("title")}
					class="px-3 py-2.5 text-left font-medium"
				>
					<button
						type="button"
						onclick={() => toggle("title")}
						class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
					>
						Title
						{#if sort === "title"}
							{#if order === "asc"}
								<ChevronUp size={12} aria-hidden="true" />
							{:else}
								<ChevronDown size={12} aria-hidden="true" />
							{/if}
						{/if}
					</button>
				</th>
				<th
					scope="col"
					aria-sort={ariaSort("year")}
					class="w-24 px-3 py-2.5 text-left font-medium"
				>
					<button
						type="button"
						onclick={() => toggle("year")}
						class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
					>
						Year
						{#if sort === "year"}
							{#if order === "asc"}
								<ChevronUp size={12} aria-hidden="true" />
							{:else}
								<ChevronDown size={12} aria-hidden="true" />
							{/if}
						{/if}
					</button>
				</th>
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">Status</th>
				<th
					scope="col"
					class="hidden w-40 px-3 py-2.5 text-left font-medium md:table-cell"
				>
					Quality
				</th>
				<th
					scope="col"
					class="hidden w-24 px-3 py-2.5 text-right font-medium md:table-cell"
				>
					Size
				</th>
				<th scope="col" class="w-12 px-3 py-2.5" aria-hidden="true"></th>
			</tr>
		</thead>
		<tbody>
			{#each movies as movie (movie.id)}
				<tr
					class="group border-b border-border last:border-b-0 transition hover:bg-surface"
				>
					<td class="px-3 py-2">
						<a
							href="/movies/{movie.id}"
							class="relative block aspect-[2/3] w-10 overflow-hidden rounded focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							<div class="absolute inset-0 bg-bg-card"></div>
							<div
								class="absolute inset-0 grid place-items-center text-fg-faint"
							>
								<Film class="h-4 w-4" aria-hidden="true" />
							</div>
							<Poster
								src={`/posters/movies/${movie.id}/poster.jpg`}
								alt=""
								class="relative h-full w-full object-cover"
							/>
						</a>
					</td>
					<td class="min-w-0 px-3 py-2">
						<a
							href="/movies/{movie.id}"
							class="block truncate font-medium text-fg transition hover:text-accent-text focus:outline-none focus-visible:underline"
						>
							{movie.title}
						</a>
						{#if movie.original_title.trim() && movie.original_title.trim() !== movie.title.trim()}
							<p class="truncate text-xs italic text-fg-faint">
								{movie.original_title}
							</p>
						{/if}
					</td>
					<td class="px-3 py-2 font-mono text-xs tabular text-fg-muted">
						{movie.year}
					</td>
					<td class="px-3 py-2">
						<StatusPill
							status={movie.status}
							size="sm"
							live={movie.status === "downloading"}
						/>
					</td>
					<td
						class="hidden px-3 py-2 font-mono text-xs text-fg-muted md:table-cell"
					>
						{quality(movie.media_files)}
					</td>
					<td
						class={cn(
							"hidden px-3 py-2 text-right font-mono text-xs tabular md:table-cell",
							totalSize(movie.media_files) === "—"
								? "text-fg-faint"
								: "text-fg-muted",
						)}
					>
						{totalSize(movie.media_files)}
					</td>
					<td class="px-3 py-2">
						<div class="flex items-center justify-end gap-1">
							<button
								type="button"
								onclick={() => monitor.mutate(movie)}
								aria-label={movie.monitored ? "Stop monitoring" : "Monitor"}
								aria-pressed={movie.monitored}
								title={movie.monitored ? "Stop monitoring" : "Monitor"}
								class={cn(
									"grid h-8 w-8 place-items-center rounded-md transition hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
									movie.monitored
										? "text-accent-text"
										: "text-fg-muted hover:text-fg",
								)}
							>
								<Bookmark
									size={15}
									fill={movie.monitored ? "currentColor" : "none"}
									aria-hidden="true"
								/>
							</button>
							<MovieActionsMenu {movie} variant="toolbar" />
						</div>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
