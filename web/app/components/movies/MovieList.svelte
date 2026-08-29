<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Film, ChevronUp, ChevronDown, Bookmark } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dragScroll } from "../../lib/drag-scroll";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import Poster from "./Poster.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import SelectBox from "../shared/SelectBox.svelte";
	import MovieActionsMenu from "./MovieActionsMenu.svelte";
	import type { Movie, MovieFileSummary } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type SortKey = "title" | "year";
	type SortOrder = "asc" | "desc";

	let {
		movies,
		sort,
		order,
		onSortChange,
		selected,
		onToggle,
		onToggleAll,
	}: {
		movies: Movie[];
		sort: SortKey;
		order: SortOrder;
		onSortChange: (s: SortKey, o: SortOrder) => void;
		selected: Set<number>;
		onToggle: (id: number, v: boolean) => void;
		onToggleAll: (v: boolean) => void;
	} = $props();

	let pageSelected = $derived(movies.filter((m) => selected.has(m.id)).length);
	let allSelected = $derived(movies.length > 0 && pageSelected === movies.length);

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

	// Both columns read the list response's file rollup, not media_files —
	// which is detail-only. totalSize is now the sum of every attached file
	// rather than the first one's size, which is what the column always meant.
	function totalSize(s: MovieFileSummary | undefined): string {
		return s ? formatBytes(s.size_bytes) : "—";
	}

	function quality(s: MovieFileSummary | undefined): string {
		if (!s) return "—";
		const parts = [s.resolution, s.codec].filter((v): v is string =>
			Boolean(v),
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

<!-- Columns hide on CONTAINER width, not viewport width: at tablet the table has
     ~700px of page, where viewport media queries kept every column and the
     right-hand ones fell outside the box. -->
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
						label={allSelected ? i18n.common_deselect_all() : i18n.common_select_all()}
					/>
				</th>
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
				<th scope="col" class="w-28 px-3 py-2.5 text-left font-medium">{i18n.common_status()}</th>
				<th
					scope="col"
					class="hidden w-40 px-3 py-2.5 text-left font-medium @3xl:table-cell"
				>
					{i18n.common_quality()}
				</th>
				<th
					scope="col"
					class="hidden w-24 px-3 py-2.5 text-right font-medium @2xl:table-cell"
				>
					{i18n.common_size()}
				</th>
				<th scope="col" class="w-12 px-3 py-2.5" aria-hidden="true"></th>
			</tr>
		</thead>
		<tbody>
			{#each movies as movie (movie.id)}
				{@const isSel = selected.has(movie.id)}
				<tr
					class={cn(
						"group border-b border-border last:border-b-0 transition",
						isSel ? "bg-accent-soft" : "hover:bg-surface",
					)}
				>
					<td class="pl-3 pr-0 py-2">
						<SelectBox
							checked={isSel}
							onChange={(v) => onToggle(movie.id, v)}
							label={isSel ? `Deselect ${movie.title}` : i18n.a11y_select_item({ title: movie.title })}
						/>
					</td>
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
							status={movieStatus(movie)}
							size="sm"
							live={movie.status === "downloading"}
						/>
					</td>
					<td
						class="hidden px-3 py-2 font-mono text-xs text-fg-muted @3xl:table-cell"
					>
						{quality(movie.file_summary)}
					</td>
					<td
						class={cn(
							"hidden px-3 py-2 text-right font-mono text-xs tabular @2xl:table-cell",
							totalSize(movie.file_summary) === "—"
								? "text-fg-faint"
								: "text-fg-muted",
						)}
					>
						{totalSize(movie.file_summary)}
					</td>
					<td class="px-3 py-2">
						<div class="flex items-center justify-end gap-1">
							<button
								type="button"
								onclick={() => monitor.mutate(movie)}
								aria-label={movie.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
								aria-pressed={movie.monitored}
								title={movie.monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
								class={cn(
									"grid h-11 w-11 lg:h-8 lg:w-8 place-items-center rounded-md transition hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
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
