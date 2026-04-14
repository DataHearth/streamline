<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Bookmark, Tv } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { tvPosterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import SeriesActionsMenu from "./SeriesActionsMenu.svelte";
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

	function availability(s: TVShow): "wanted" | "available" {
		return (s.wanted_episodes ?? 0) > 0 ? "wanted" : "available";
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
				<th scope="col" class="px-3 py-2.5 text-left font-medium">Title</th>
				<th
					scope="col"
					class="hidden w-28 px-3 py-2.5 text-left font-medium md:table-cell"
				>
					Network
				</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-left font-medium sm:table-cell"
				>
					Type
				</th>
				<th scope="col" class="w-32 px-3 py-2.5 text-left font-medium">Status</th>
				<th scope="col" class="w-52 px-3 py-2.5 text-left font-medium">
					Episodes
				</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-right font-medium sm:table-cell"
				>
					Rating
				</th>
				<th scope="col" class="w-20 px-3 py-2.5" aria-hidden="true"></th>
			</tr>
		</thead>
		<tbody>
			{#each series as show (show.id)}
				{@const avail = availability(show)}
				<tr
					class="group border-b border-border last:border-b-0 transition hover:bg-surface"
				>
					<td class="px-3 py-2.5">
						<a
							href="/series/{show.id}"
							class="relative block aspect-[2/3] w-10 overflow-hidden rounded focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							<div class="absolute inset-0 bg-bg-card"></div>
							<div
								class="absolute inset-0 grid place-items-center text-fg-faint"
							>
								<Tv class="h-4 w-4" aria-hidden="true" />
							</div>
							<Poster
								src={tvPosterUrl(show.id)}
								alt=""
								class="relative h-full w-full object-cover"
							/>
						</a>
					</td>
					<td class="min-w-0 px-3 py-2.5">
						<a
							href="/series/{show.id}"
							class="block truncate font-medium text-fg transition hover:text-accent-text focus:outline-none focus-visible:underline"
						>
							{show.title}
						</a>
						{#if show.original_title?.trim() && show.original_title.trim() !== show.title.trim()}
							<p class="truncate text-xs italic text-fg-faint">
								{show.original_title}
							</p>
						{/if}
					</td>
					<td
						class="hidden px-3 py-2.5 font-mono text-xs text-fg-muted md:table-cell"
					>
						{show.network ?? "—"}
					</td>
					<td
						class={cn(
							"hidden px-3 py-2.5 font-mono text-xs lowercase sm:table-cell",
							show.type === "anime"
								? "text-accent-text"
								: show.type === "daily"
									? "text-status-grabbing"
									: "text-fg-muted",
						)}
					>
						{show.type}
					</td>
					<td class="px-3 py-2.5">
						<span class="inline-flex items-center gap-2">
							<span
								class="h-[7px] w-[7px] shrink-0 rounded-full"
								style:background-color={`var(--status-${avail})`}
							></span>
							<span class="font-mono text-xs text-fg-muted">
								{show.series_status}
							</span>
						</span>
					</td>
					<td
						class="whitespace-nowrap px-3 py-2.5 font-mono text-xs tabular text-fg"
					>
						{show.have_episodes ?? 0}<span class="text-fg-muted"
							>/{show.total_episodes ?? 0}</span
						>
						{#if (show.wanted_episodes ?? 0) > 0}
							<span class="ml-1.5 text-status-wanted"
								>· {show.wanted_episodes} wanted</span
							>
						{/if}
					</td>
					<td
						class="hidden px-3 py-2.5 text-right font-mono text-xs tabular text-fg-muted sm:table-cell"
					>
						{#if show.rating && show.rating > 0}
							★ {show.rating.toFixed(1)}
						{:else}
							—
						{/if}
					</td>
					<td class="px-3 py-2.5">
						<div class="flex items-center justify-end gap-1">
							<button
								type="button"
								onclick={() => monitor.mutate(show)}
								aria-label={show.monitored ? "Stop monitoring" : "Monitor"}
								aria-pressed={show.monitored}
								title={show.monitored ? "Stop monitoring" : "Monitor"}
								class={cn(
									"grid h-8 w-8 place-items-center rounded-md transition hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
									show.monitored
										? "text-accent-text"
										: "text-fg-muted hover:text-fg",
								)}
							>
								<Bookmark
									size={15}
									fill={show.monitored ? "currentColor" : "none"}
									aria-hidden="true"
								/>
							</button>
							<SeriesActionsMenu {show} variant="toolbar" />
						</div>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
