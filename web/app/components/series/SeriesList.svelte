<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Bookmark, Tv } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { tvPosterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import SelectBox from "../shared/SelectBox.svelte";
	import SeriesActionsMenu from "./SeriesActionsMenu.svelte";
	import type { TVShow } from "../../lib/types";

	let {
		series,
		selected,
		onToggle,
		onToggleAll,
	}: {
		series: TVShow[];
		selected: Set<number>;
		onToggle: (id: number, v: boolean) => void;
		onToggleAll: (v: boolean) => void;
	} = $props();

	let pageSelected = $derived(series.filter((s) => selected.has(s.id)).length);
	let allSelected = $derived(series.length > 0 && pageSelected === series.length);

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

	function availability(s: TVShow): "wanted" | "available" | "missing" {
		if ((s.wanted_episodes ?? 0) > 0) return "wanted";
		// Unmonitored shows report zero wanted episodes, so "nothing wanted" alone
		// would badge an empty library entry as available.
		return (s.have_episodes ?? 0) > 0 ? "available" : "missing";
	}
</script>

<!-- Columns hide on CONTAINER width, not viewport width: at tablet the table has
     ~700px of page (and less inside a pane), where viewport media queries kept
     every column and the right-hand ones fell outside the box. -->
<div
	use:dragScroll
	class="@container overflow-x-auto overflow-y-hidden rounded-lg border border-border bg-bg-elevated/70 backdrop-blur-md"
>
	<table class="w-full text-sm">
		<thead
			class="bg-bg-elevated/95 text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<tr class="border-b border-border">
				<th scope="col" class="w-10 pl-3 pr-0 py-2.5">
					<SelectBox
						checked={allSelected}
						indeterminate={pageSelected > 0 && !allSelected}
						onChange={(v) => onToggleAll(v)}
						label={allSelected ? "Deselect all" : "Select all"}
					/>
				</th>
				<th scope="col" class="w-12 px-3 py-2.5" aria-hidden="true"></th>
				<th scope="col" class="px-3 py-2.5 text-left font-medium">Title</th>
				<th
					scope="col"
					class="hidden w-28 px-3 py-2.5 text-left font-medium @4xl:table-cell"
				>
					Network
				</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-left font-medium @3xl:table-cell"
				>
					Type
				</th>
				<th scope="col" class="w-32 px-3 py-2.5 text-left font-medium">Status</th>
				<th scope="col" class="w-44 px-3 py-2.5 text-left font-medium @2xl:w-52">
					Episodes
				</th>
				<th
					scope="col"
					class="hidden w-20 px-3 py-2.5 text-right font-medium @2xl:table-cell"
				>
					Rating
				</th>
				<th scope="col" class="w-20 px-3 py-2.5" aria-hidden="true"></th>
			</tr>
		</thead>
		<tbody>
			{#each series as show (show.id)}
				{@const avail = availability(show)}
				{@const isSel = selected.has(show.id)}
				<tr
					class={cn(
						"group border-b border-border last:border-b-0 transition",
						isSel ? "bg-accent-soft" : "hover:bg-surface",
					)}
				>
					<td class="pl-3 pr-0 py-2.5">
						<SelectBox
							checked={isSel}
							onChange={(v) => onToggle(show.id, v)}
							label={isSel ? `Deselect ${show.title}` : `Select ${show.title}`}
						/>
					</td>
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
						class="hidden px-3 py-2.5 font-mono text-xs text-fg-muted @4xl:table-cell"
					>
						{show.network ?? "—"}
					</td>
					<td
						class={cn(
							"hidden px-3 py-2.5 font-mono text-xs lowercase @3xl:table-cell",
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
						class="hidden px-3 py-2.5 text-right font-mono text-xs tabular text-fg-muted @2xl:table-cell"
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
