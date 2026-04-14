<script lang="ts">
	import {
		createQuery,
		useQueryClient,
		createMutation,
	} from "@tanstack/svelte-query";
	import { params, goto } from "@roxi/routify";
	import { onMount } from "svelte";
	import {
		Tv,
		ArrowLeft,
		Bookmark,
		Eye,
		Search,
		ExternalLink,
		Trash2,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import { tvPosterUrl } from "../../lib/posters";
	import Poster from "../../components/movies/Poster.svelte";
	import StatusPill from "../../components/shared/StatusPill.svelte";
	import type { StatusKind } from "../../components/shared/StatusPill.svelte";
	import ProgressBar from "../../components/shared/ProgressBar.svelte";
	import Select from "../../components/forms/Select.svelte";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import SeasonStrip from "../../components/series/SeasonStrip.svelte";
	import SeasonGrid from "../../components/series/SeasonGrid.svelte";
	import EpisodeTable from "../../components/series/EpisodeTable.svelte";
	import SeriesFiles from "../../components/series/SeriesFiles.svelte";
	import SeriesManualSearchModal from "../../components/series/SeriesManualSearchModal.svelte";
	import SeriesReleaseSearchModal from "../../components/series/SeriesReleaseSearchModal.svelte";
	import SeriesKebabMenu from "../../components/series/SeriesKebabMenu.svelte";
	import MovieDetailCast from "../../components/movies/MovieDetailCast.svelte";
	import DetailAbout from "../../components/shared/DetailAbout.svelte";
	import PlayOnMenu from "../../components/movies/PlayOnMenu.svelte";
	import type { SeriesAction } from "../../components/series/SeriesKebabMenu.svelte";
	import type {
		Episode,
		MonitoringPreset,
		Season,
		TVShow,
	} from "../../lib/types";

	type Tab = "overview" | "episodes" | "seasons" | "files" | "history" | "cast";
	const TABS: { key: Tab; label: string }[] = [
		{ key: "overview", label: "Overview" },
		{ key: "episodes", label: "Episodes" },
		{ key: "seasons", label: "Seasons" },
		{ key: "files", label: "Files" },
		{ key: "history", label: "History" },
		{ key: "cast", label: "Cast" },
	];
	const VALID_TABS = new Set<Tab>([
		"overview",
		"episodes",
		"seasons",
		"files",
		"history",
		"cast",
	]);

	let routeParams = $state<Record<string, string>>({});
	let navigate = $state<(path: string) => void>(() => {});
	onMount(() => {
		const u1 = params.subscribe((p) => (routeParams = p));
		const u2 = goto.subscribe((fn) => (navigate = fn));
		return () => {
			u1();
			u2();
		};
	});
	const seriesId = $derived(Number(routeParams.id));

	function readTab(): Tab {
		if (typeof window === "undefined") return "overview";
		const t = new URLSearchParams(window.location.search).get("tab");
		return t && VALID_TABS.has(t as Tab) ? (t as Tab) : "overview";
	}
	let tab = $state<Tab>(readTab());

	$effect(() => {
		if (typeof window === "undefined") return;
		const p = new URLSearchParams(window.location.search);
		if (tab === "overview") p.delete("tab");
		else p.set("tab", tab);
		const search = p.toString();
		const next = `${window.location.pathname}${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	const seriesQuery = createQuery<TVShow>(() => ({
		queryKey: ["series", seriesId],
		queryFn: () => api<TVShow>(`/series/${seriesId}`),
		enabled: Number.isFinite(seriesId) && seriesId > 0,
	}));

	let show = $derived(seriesQuery.data);
	let seasons = $derived<Season[]>(show?.seasons ?? []);

	let selectedSeason = $state<number | null>(null);
	$effect(() => {
		if (selectedSeason === null && seasons.length > 0) {
			// Default to the latest non-special season, falling back to whatever
			// the last entry is (e.g. a specials-only show).
			const regular = seasons.filter((s) => s.number > 0);
			const pool = regular.length > 0 ? regular : seasons;
			const last = pool[pool.length - 1];
			if (last) selectedSeason = last.number;
		}
	});
	let currentSeason = $derived(
		seasons.find((s) => s.number === selectedSeason) ?? null,
	);
	let currentEpisodes = $derived<Episode[]>(currentSeason?.episodes ?? []);

	let seriesAvail = $derived<StatusKind>(
		(show?.wanted_episodes ?? 0) > 0 ? "wanted" : "available",
	);

	let airedTotal = $derived.by(() => {
		if (seasons.length > 0) {
			return seasons.reduce(
				(n, s) => n + Math.max(0, (s.total ?? 0) - (s.unaired ?? 0)),
				0,
			);
		}
		return show?.total_episodes ?? 0;
	});
	let unairedTotal = $derived(
		seasons.reduce((n, s) => n + (s.unaired ?? 0), 0),
	);
	let seriesProgress = $derived(
		(show?.have_episodes ?? 0) / Math.max(1, airedTotal),
	);

	let metaParts = $derived.by(() => {
		if (!show) return [] as string[];
		const p: string[] = [];
		p.push(String(show.year));
		if (seasons.length > 0)
			p.push(`${seasons.length} season${seasons.length === 1 ? "" : "s"}`);
		if (show.total_episodes) p.push(`${show.total_episodes} episodes`);
		if (show.rating && show.rating > 0) p.push(`★ ${show.rating.toFixed(1)}`);
		if (show.runtime) p.push(`${show.runtime}m`);
		if (show.genres?.length) p.push(show.genres.join(" / "));
		return p;
	});

	const presetOptions: { value: MonitoringPreset; label: string }[] = [
		{ value: "all", label: "All episodes" },
		{ value: "future", label: "Future episodes" },
		{ value: "missing", label: "Missing episodes" },
		{ value: "existing", label: "Existing episodes" },
		{ value: "pilot", label: "Pilot only" },
		{ value: "none", label: "None" },
	];
	// The backend applies a preset as a one-shot bulk toggle; it stores no
	// ongoing "monitoring mode", so this control has no persisted value to
	// reflect. Start unselected-ish on "all" and treat each pick as an action.
	let presetValue = $state<MonitoringPreset>("all");

	let deleteOpen = $state(false);
	let deleteWithFilesOpen = $state(false);
	let manualOpen = $state(false);
	let manualEpisode = $state<Episode | null>(null);
	let packSearchOpen = $state(false);
	// One delete flow drives episode / season / series scope: the target holds
	// a label for the confirm copy plus the episodes whose files get removed.
	let deleteFiles = $state<{ label: string; episodes: Episode[] } | null>(null);
	let removeFilesTorrent = $state(false);

	function pad2(n: number): string {
		return String(n).padStart(2, "0");
	}
	function openManualSearch(ep: Episode) {
		manualEpisode = ep;
		manualOpen = true;
	}
	function openDeleteFiles(label: string, episodes: Episode[]) {
		if (episodes.length === 0) return;
		deleteFiles = { label, episodes };
		removeFilesTorrent = false;
	}
	function episodeCode(ep: Episode): string {
		return currentSeason
			? `S${pad2(currentSeason.number)}E${pad2(ep.number)}`
			: `Episode ${ep.number}`;
	}
	let manualScope = $derived.by(() => {
		if (!manualEpisode || currentSeason === null) return undefined;
		const code = `S${pad2(currentSeason.number)}E${pad2(manualEpisode.number)}`;
		return manualEpisode.title
			? `${code} — ${manualEpisode.title}`
			: code;
	});

	const qc = useQueryClient();
	function invalidate() {
		qc.invalidateQueries({ queryKey: ["series"] });
	}

	const monitor = createMutation<TVShow, Error, boolean>(() => ({
		mutationFn: (next) =>
			api<TVShow>(`/series/${seriesId}`, {
				method: "PATCH",
				body: { monitored: next },
			}),
		onSuccess: (_d, next) => {
			invalidate();
			toast.ok(next ? "Now monitoring" : "Stopped monitoring");
		},
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	const applyPreset = createMutation<TVShow, Error, MonitoringPreset>(() => ({
		mutationFn: (p) =>
			api<TVShow>(`/series/${seriesId}`, {
				method: "PATCH",
				body: { preset: p },
			}),
		onSuccess: (_d, p) => {
			invalidate();
			const label =
				presetOptions.find((o) => o.value === p)?.label ?? p;
			toast.ok(`Monitoring set to ${label.toLowerCase()}`);
		},
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	const refresh = createMutation(() => ({
		mutationFn: () =>
			api<TVShow>(`/series/${seriesId}/refresh-metadata`, { method: "POST" }),
		onSuccess: () => {
			invalidate();
			toast.ok("Metadata refresh requested");
		},
		onError: (e: Error) => toast.err(e.message ?? "Refresh failed"),
	}));

	const searchSeries = createMutation(() => ({
		mutationFn: () => api(`/series/${seriesId}/search`, { method: "POST" }),
		onSuccess: () => toast.ok("Search dispatched for wanted episodes"),
		onError: (e: Error) => toast.err(e.message ?? "Search failed"),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/series/${seriesId}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			invalidate();
			toast.ok("Series deleted");
			navigate("/series");
		},
		onError: (e: Error) => toast.err(e.message ?? "Delete failed"),
	}));

	const monitorSeason = createMutation<unknown, Error, Season>(() => ({
		mutationFn: (s) =>
			api(`/series/${seriesId}/seasons/${s.number}`, {
				method: "PATCH",
				body: { monitored: !s.monitored },
			}),
		onSuccess: (_d, s) => {
			invalidate();
			toast.ok(s.monitored ? "Season unmonitored" : "Season monitored");
		},
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	const monitorEpisode = createMutation<unknown, Error, Episode>(() => ({
		mutationFn: (ep) =>
			api(`/series/${seriesId}/episodes/${ep.id}`, {
				method: "PATCH",
				body: { monitored: !ep.monitored },
			}),
		onSuccess: () => invalidate(),
		onError: (e) => toast.err(e.message ?? "Update failed"),
	}));

	const delFiles = createMutation<unknown, Error, { episodes: Episode[]; remove: boolean }>(
		() => ({
			// ponytail: sequential per-episode DELETEs (no bulk endpoint). Fine for a
			// season; add DELETE /series/{id}/files if a whole downloaded series is
			// too slow.
			mutationFn: async ({ episodes, remove }) => {
				for (const ep of episodes) {
					await api(`/series/${seriesId}/episodes/${ep.id}/file`, {
						method: "DELETE",
						body: { remove_torrent: remove },
					});
				}
			},
			onSuccess: (_d, { episodes }) => {
				invalidate();
				toast.ok(episodes.length > 1 ? "Files deleted" : "File deleted");
				deleteFiles = null;
			},
			onError: (e: Error) => toast.err(e.message ?? "Delete failed"),
		}),
	);

	function onKebabPick(a: SeriesAction) {
		if (a === "search") searchSeries.mutate();
		else if (a === "refresh") refresh.mutate();
		else if (a === "delete") deleteOpen = true;
		else if (a === "delete-with-files") deleteWithFilesOpen = true;
		else if (a === "delete-files") openDeleteFiles("this series", seriesFileEpisodes);
	}

	let hasFiles = $derived((show?.have_episodes ?? 0) > 0);
	let seasonFileEpisodes = $derived(
		currentEpisodes.filter((e) => (e.size ?? 0) > 0),
	);
	let seriesFileEpisodes = $derived(
		seasons.flatMap((s) => s.episodes ?? []).filter((e) => (e.size ?? 0) > 0),
	);
	const seasonLabel = "Season";
	let searchSeasons = $derived(
		seasons
			.filter((s) => (s.total ?? 0) > 0)
			.map((s) => ({
				number: s.number,
				label: s.number === 0 ? "Specials" : `${seasonLabel} ${s.number}`,
			})),
	);
	let qpName = $derived(show?.quality_profile || "Server default");
</script>

{#if seriesQuery.isLoading}
	<section class="relative overflow-hidden bg-bg-deep">
		<div class="flex w-full items-stretch gap-10 px-4 py-16 md:px-8">
			<div
				class="aspect-[2/3] w-[200px] animate-pulse rounded-lg bg-bg-card/60 motion-reduce:animate-none"
			></div>
			<div class="flex flex-1 flex-col gap-3">
				<div
					class="h-8 w-2/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
				<div
					class="h-5 w-1/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
				<div
					class="mt-auto h-24 w-full animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
			</div>
		</div>
	</section>
{:else if seriesQuery.isError}
	<div
		class="mx-4 mt-4 rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center md:mx-8"
	>
		<p class="text-sm font-semibold text-status-failed">Failed to load series</p>
		<p class="mt-1 text-xs text-fg-subtle">
			{seriesQuery.error?.message ?? "Unknown error"}
		</p>
	</div>
{:else if show}
	<section class="hero relative isolate" aria-labelledby="series-title">
		<div class="absolute inset-0 -z-10 overflow-hidden bg-bg-deep">
			<img
				src={tvPosterUrl(show.id)}
				alt=""
				aria-hidden="true"
				class="h-full w-full scale-110 object-cover opacity-70 blur-md"
			/>
			<div class="absolute inset-0 hero-overlay"></div>
		</div>

		<div class="relative w-full px-4 pt-6 md:px-8">
			<a
				href="/series"
				class="inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-3 py-1.5 text-[11.5px] font-medium text-fg-muted backdrop-blur-sm transition hover:bg-black/60 hover:text-fg"
			>
				<ArrowLeft size={13} aria-hidden="true" />
				Series
			</a>
		</div>

		<div
			class="relative grid w-full items-end gap-6 px-4 pb-12 pt-8 md:grid-cols-[260px_1fr] md:gap-10 md:px-8 md:pb-16 md:pt-10"
		>
			<div
				class="relative aspect-[2/3] w-40 overflow-hidden rounded-lg shadow-[0_24px_48px_rgb(0_0_0_/0.5)] md:w-auto"
			>
				<div class="absolute inset-0 bg-bg-card"></div>
				<div class="absolute inset-0 grid place-items-center text-fg-faint">
					<Tv class="h-10 w-10" aria-hidden="true" />
				</div>
				<Poster
					src={tvPosterUrl(show.id)}
					alt="{show.title} poster"
					loading="eager"
					class="relative h-full w-full object-cover"
				/>
			</div>

			<div class="min-w-0 text-left">
				<div
					class="mb-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
				>
					<StatusPill status={seriesAvail} size="md" variant="translucent" />
					<span class="uppercase tracking-wide">{show.series_status}</span>
					{#if show.network}
						<span class="text-fg-faint" aria-hidden="true">·</span>
						<span>{show.network}</span>
					{/if}
					{#if show.creator}
						<span class="text-fg-faint" aria-hidden="true">·</span>
						<span>{show.creator}</span>
					{/if}
					<span
						class={cn(
							"rounded-full px-2 py-0.5 text-[10px] uppercase tracking-wide",
							show.type === "anime"
								? "bg-accent-soft text-accent-text"
								: show.type === "daily"
									? "bg-status-grabbing/15 text-status-grabbing"
									: "bg-white/[0.06] text-fg-subtle",
						)}
					>
						{show.type}
					</span>
				</div>

				<h1
					id="series-title"
					class="text-3xl font-bold leading-[1.05] tracking-tight text-fg md:text-5xl"
					title={show.title}
				>
					{show.title}
				</h1>

				{#if metaParts.length > 0}
					<div
						class="mt-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted"
					>
						{#each metaParts as part, i (i)}
							{#if i > 0}
								<span class="text-fg-faint" aria-hidden="true">·</span>
							{/if}
							<span>{part}</span>
						{/each}
					</div>
				{/if}

				{#if show.overview}
					<p
						class="mt-4 line-clamp-3 max-w-[680px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
					>
						{show.overview}
					</p>
				{/if}

				<div class="mt-5 max-w-[680px]">
					<div
						class="mb-1.5 flex items-center gap-1.5 font-mono text-xs text-fg-muted"
					>
						<span class="text-fg">{show.have_episodes ?? 0}</span>
						<span class="text-fg-faint">/</span>
						<span>{show.total_episodes ?? 0}</span>
						<span class="text-fg-subtle">episodes</span>
						{#if (show.wanted_episodes ?? 0) > 0}
							<span class="text-status-wanted">· {show.wanted_episodes} wanted</span>
						{/if}
						{#if unairedTotal > 0}
							<span class="text-fg-faint">· {unairedTotal} unaired</span>
						{/if}
					</div>
					<ProgressBar
						value={seriesProgress}
						status="available"
						height={4}
						label="Series progress"
					/>
				</div>

				<div
					class="mt-5 flex flex-wrap items-center gap-2.5"
					aria-label="Series actions"
				>
					<PlayOnMenu
						path={`/series/${show.id}/play-on`}
						queryKey={["series", show.id, "play-on"]}
						disabled={!hasFiles}
						disabledTitle="Available once episodes are imported"
					/>

					<button
						type="button"
						onclick={() => (packSearchOpen = true)}
						class="inline-flex h-10 items-center gap-2 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow"
					>
						<Search size={14} aria-hidden="true" />
						Manual search
					</button>

					<div class="flex items-center gap-2">
						<label
							for="series-monitor-preset"
							class="inline-flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-fg-subtle"
						>
							<Eye size={14} aria-hidden="true" />
							Monitor
						</label>
						<div class="w-44">
							<Select
								id="series-monitor-preset"
								value={presetValue}
								options={presetOptions}
								onChange={(v) => {
									presetValue = v;
									applyPreset.mutate(v);
								}}
							/>
						</div>
					</div>

					<button
						type="button"
						onclick={() => monitor.mutate(!(show.monitored ?? false))}
						disabled={monitor.isPending}
						aria-pressed={show.monitored ?? false}
						title={show.monitored ? "Stop monitoring" : "Monitor"}
						class="inline-flex h-10 w-10 items-center justify-center rounded-md border border-border-strong bg-white/[0.08] text-fg backdrop-blur-sm transition hover:bg-white/[0.14] disabled:cursor-not-allowed disabled:opacity-60"
					>
						<Bookmark
							size={16}
							fill={show.monitored ? "currentColor" : "none"}
							aria-hidden="true"
						/>
						<span class="sr-only">
							{show.monitored ? "Stop monitoring" : "Monitor"}
						</span>
					</button>

					<SeriesKebabMenu
						onPick={onKebabPick}
						allowDeleteFiles
						disabledActions={hasFiles ? [] : ["delete-with-files", "delete-files"]}
					/>
				</div>
			</div>
		</div>
	</section>

	<nav
		aria-label="Series sections"
		class="sticky top-14 z-10 border-b border-border bg-bg-deep/70 px-4 backdrop-blur-md saturate-150 md:px-8"
	>
		<div class="tabs-track flex w-full gap-0.5">
			{#each TABS as t (t.key)}
				{@const active = tab === t.key}
				<button
					type="button"
					onclick={() => (tab = t.key)}
					aria-current={active ? "page" : undefined}
					class={cn(
						"relative -mb-px shrink-0 px-4 py-3.5 text-[13px] font-medium transition",
						active ? "text-fg" : "text-fg-subtle hover:text-fg",
					)}
				>
					<span>{t.label}</span>
					{#if t.key === "episodes"}
						<span class="ml-1.5 font-mono text-[11px] text-fg-faint">
							{show.have_episodes ?? 0}/{show.total_episodes ?? 0}
						</span>
					{:else if t.key === "seasons" && seasons.length > 0}
						<span class="ml-1.5 font-mono text-[11px] text-fg-faint">
							{seasons.length}
						</span>
					{/if}
					{#if active}
						<span
							aria-hidden="true"
							class="absolute inset-x-3 -bottom-px h-0.5 rounded-t-sm bg-accent"
						></span>
					{/if}
				</button>
			{/each}
		</div>
	</nav>

	<div class="w-full px-4 py-6 md:px-8">
		{#if tab === "overview"}
			<div class="grid grid-cols-1 gap-6 lg:grid-cols-[1fr_320px] lg:gap-10">
				<DetailAbout
					overview={show.overview}
					cast={show.cast ?? []}
					onViewAllCast={() => (tab = "cast")}
				/>

				<aside class="flex flex-col gap-4">
					<section
						class="rounded-lg border border-border bg-bg-elevated p-5"
						aria-labelledby="series-info-library"
					>
						<h4
							id="series-info-library"
							class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
						>
							Library
						</h4>
						<dl
							class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]"
						>
							<dt class="text-fg-subtle">Quality profile</dt>
							<dd class="text-right font-mono text-fg">{qpName}</dd>
							<dt class="text-fg-subtle">Status</dt>
							<dd class="text-right font-mono text-fg capitalize">
								{show.series_status}
							</dd>
							<dt class="text-fg-subtle">Monitored</dt>
							<dd class="text-right font-mono text-fg">
								{show.monitored ? "Yes" : "No"}
							</dd>
							<dt class="text-fg-subtle">Episodes</dt>
							<dd class="text-right font-mono text-fg">
								{show.have_episodes ?? 0}/{show.total_episodes ?? 0}
							</dd>
							{#if show.network}
								<dt class="text-fg-subtle">Network</dt>
								<dd class="text-right font-mono text-fg">{show.network}</dd>
							{/if}
							{#if show.year}
								<dt class="text-fg-subtle">Year</dt>
								<dd class="text-right font-mono text-fg">{show.year}</dd>
							{/if}
							{#if show.runtime}
								<dt class="text-fg-subtle">Runtime</dt>
								<dd class="text-right font-mono text-fg">{show.runtime}m</dd>
							{/if}
							<dt class="text-fg-subtle">TVDB</dt>
							<dd class="text-right">
								<a
									href="https://www.thetvdb.com/dereferrer/series/{show.tvdb_id}"
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 font-mono text-accent-text transition hover:text-accent"
								>
									{show.tvdb_id}
									<ExternalLink size={11} aria-hidden="true" />
								</a>
							</dd>
						</dl>
					</section>
				</aside>
			</div>
		{:else if tab === "episodes"}
			{#if seasons.length === 0}
				<p class="py-12 text-center text-sm text-fg-subtle">
					No seasons found for this series.
				</p>
			{:else}
				<div class="flex flex-col gap-5">
					<SeasonStrip
						{seasons}
						selected={selectedSeason ?? seasons[0]?.number ?? 0}
						onSelect={(n) => (selectedSeason = n)}
					/>

					{#if currentSeason}
						<div class="flex flex-wrap items-center justify-between gap-3">
							<div>
								<h2 class="text-lg font-semibold text-fg">
									{currentSeason.number === 0
										? "Specials"
										: `${seasonLabel} ${currentSeason.number}`}
									{#if currentSeason.name && currentSeason.number !== 0}
										<span class="text-fg-subtle">· {currentSeason.name}</span>
									{/if}
								</h2>
								<p class="mt-0.5 font-mono text-xs text-fg-muted">
									{currentSeason.total ?? 0} episodes
									<span class="text-fg-faint">·</span>
									{currentSeason.available ?? 0} available
									{#if (currentSeason.missing ?? 0) > 0}
										<span class="text-fg-faint">·</span>
										<span class="text-status-wanted"
											>{currentSeason.missing} missing</span
										>
									{/if}
									{#if (currentSeason.unaired ?? 0) > 0}
										<span class="text-fg-faint">·</span>
										<span class="text-fg-faint">{currentSeason.unaired} unaired</span>
									{/if}
								</p>
							</div>
							<div class="flex items-center gap-2">
								{#if seasonFileEpisodes.length > 0}
									<button
										type="button"
										onclick={() =>
											currentSeason &&
											openDeleteFiles(
												currentSeason.number === 0
													? "Specials"
													: `${seasonLabel} ${currentSeason.number}`,
												seasonFileEpisodes,
											)}
										class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-sm text-fg-muted transition hover:border-status-failed/40 hover:bg-status-failed/10 hover:text-status-failed"
									>
										<Trash2 size={15} aria-hidden="true" />
										Delete files
									</button>
								{/if}
								<button
									type="button"
									onclick={() =>
										currentSeason && monitorSeason.mutate(currentSeason)}
									aria-pressed={currentSeason?.monitored}
									title={currentSeason?.monitored
										? "Stop monitoring season"
										: "Monitor season"}
									class={cn(
										"grid h-9 w-9 place-items-center rounded-md border border-border bg-bg-elevated transition hover:border-border-strong",
										currentSeason.monitored
											? "text-accent-text"
											: "text-fg-subtle hover:text-fg",
									)}
								>
									<Bookmark
										size={15}
										fill={currentSeason.monitored ? "currentColor" : "none"}
										aria-hidden="true"
									/>
									<span class="sr-only">
										{currentSeason.monitored
											? "Stop monitoring season"
											: "Monitor season"}
									</span>
								</button>
							</div>
						</div>

						<EpisodeTable
							episodes={currentEpisodes}
							seasonNumber={currentSeason.number}
							seriesType={show.type}
							seasonMonitored={currentSeason.monitored}
							onMonitorEpisode={(ep) => monitorEpisode.mutate(ep)}
							onManualSearch={openManualSearch}
							onDeleteFile={(ep) => openDeleteFiles(episodeCode(ep), [ep])}
						/>
					{/if}
				</div>
			{/if}
		{:else if tab === "seasons"}
			{#if seasons.length === 0}
				<p class="py-12 text-center text-sm text-fg-subtle">
					No seasons found for this series.
				</p>
			{:else}
				<SeasonGrid
					{seasons}
					selected={selectedSeason ?? seasons[0]?.number ?? 0}
					onSelect={(n) => {
						selectedSeason = n;
						tab = "episodes";
					}}
				/>
			{/if}
		{:else if tab === "files"}
			<SeriesFiles {seasons} seriesId={show.id} />
		{:else if tab === "history"}
			<div
				class="rounded-lg border border-dashed border-border bg-bg-card/40 py-14 text-center"
			>
				<p class="text-sm font-medium text-fg-muted">No history yet</p>
				<p class="mt-1 text-xs text-fg-subtle">
					Per-series grab and import history isn't surfaced by the API yet.
				</p>
			</div>
		{:else if tab === "cast"}
			<MovieDetailCast cast={show.cast ?? []} />
		{/if}
	</div>

	<Dialog
		open={deleteOpen}
		title="Remove '{show.title}' from your library?"
		body="Files on disk will be kept."
		onClose={() => (deleteOpen = false)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label: "Delete",
				variant: "danger",
				dismiss: false,
				pending: del.isPending,
				onClick: () => del.mutate(false),
			},
		]}
	/>
	<Dialog
		open={deleteWithFilesOpen}
		title="Remove '{show.title}' and delete its files?"
		body="All downloaded episode files will be deleted from disk. This cannot be undone."
		onClose={() => (deleteWithFilesOpen = false)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label: "Delete + files",
				variant: "danger",
				dismiss: false,
				pending: del.isPending,
				onClick: () => del.mutate(true),
			},
		]}
	/>
	<SeriesManualSearchModal
		open={manualOpen}
		seriesId={show.id}
		episodeId={manualEpisode?.id ?? 0}
		scopeLabel={manualScope}
		onClose={() => (manualOpen = false)}
	/>
	<SeriesReleaseSearchModal
		open={packSearchOpen}
		seriesId={show.id}
		seasons={searchSeasons}
		onClose={() => (packSearchOpen = false)}
	/>
	<Dialog
		open={deleteFiles !== null}
		title={deleteFiles && deleteFiles.episodes.length > 1
			? `Delete all ${deleteFiles.episodes.length} files in ${deleteFiles.label}?`
			: "Delete this episode file?"}
		onClose={() => (deleteFiles = null)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label:
					deleteFiles && deleteFiles.episodes.length > 1
						? "Delete files"
						: "Delete file",
				variant: "danger",
				dismiss: false,
				pending: delFiles.isPending,
				onClick: () =>
					deleteFiles &&
					delFiles.mutate({
						episodes: deleteFiles.episodes,
						remove: removeFilesTorrent,
					}),
			},
		]}
	>
		<p class="text-sm leading-relaxed text-fg-muted">
			{#if deleteFiles && deleteFiles.episodes.length > 1}
				The files are removed from disk and their episodes revert to <span
					class="font-medium text-fg">wanted</span
				>, so the next monitored search re-grabs them.
			{:else}
				The file is removed from disk and the episode reverts to <span
					class="font-medium text-fg">wanted</span
				>, so the next monitored search re-grabs it.
			{/if}
		</p>
		<Checkbox
			checked={removeFilesTorrent}
			onChange={(v) => (removeFilesTorrent = v)}
			class="mt-4 text-sm text-fg"
		>
			Also remove the torrent{deleteFiles && deleteFiles.episodes.length > 1
				? "s"
				: ""} from the download client
		</Checkbox>
	</Dialog>
{/if}

<style>
	.hero-overlay {
		background-image: linear-gradient(
			180deg,
			rgb(11 11 16 / 0.3) 0%,
			rgb(11 11 16 / 0.7) 60%,
			var(--bg-deep) 100%
		);
	}
	.tabs-track {
		overflow-x: auto;
		overflow-y: hidden;
		scrollbar-width: none;
	}
	.tabs-track::-webkit-scrollbar {
		display: none;
	}
</style>
