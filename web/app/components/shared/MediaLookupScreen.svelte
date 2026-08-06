<script lang="ts">
	import { tick } from "svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		ChevronLeft,
		Search,
		X,
		Plus,
		Check,
		LoaderCircle,
		Film,
		Tv,
		ArrowUpRight,
		Star,
	} from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { auth } from "../../lib/auth.svelte";
	import { formatBytes } from "../../lib/format";
	import { movieStatus } from "../../lib/status";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import type {
		AddMovieRequest,
		AddSeriesRequest,
		LookupDetail,
		Movie,
		PaginatedMovies,
		PaginatedTVShows,
		QualityProfile,
		SeriesLookupResultList,
		TMDBMovieResult,
		TVShow,
	} from "../../lib/types";
	import LookupDetailPanel from "./LookupDetailPanel.svelte";
	import LookupSheet from "./LookupSheet.svelte";
	import StatusPill from "./StatusPill.svelte";
	import Select from "../forms/Select.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The touch add/request flow. Where the desktop modal is a split panel with
	// a commit button in its footer, this takes over the screen: the library's
	// own poster grid, filled with lookup results, and a sheet that peeks to
	// confirm the pick and swipes up to the full detail. One component serves
	// movies and series — the two lookups differ only in endpoint and id field.
	type Props = {
		kind: "movie" | "series";
		open: boolean;
		onClose: () => void;
	};
	let { kind, open, onClose }: Props = $props();

	type Hit = {
		id: number;
		title: string;
		year?: number;
		poster_url?: string;
		overview?: string;
		// Original title for movies, network for series.
		subtitle?: string;
		already_added?: boolean;
	};

	let isMovie = $derived(kind === "movie");
	// request_only users create a request instead of adding directly.
	let canAdd = $derived(auth.canAddDirectly);

	let query = $state("");
	let debounced = $state("");
	// "" means "let the backend resolve the default profile".
	let qualityProfileName = $state<string>("");
	let selectedId = $state<number | null>(null);
	let sheetExpanded = $state(false);
	let pendingId = $state<number | null>(null);
	// Lookup id → local id for adds made during this session; covers the gap
	// between mutation success and the library refetch.
	let sessionAdds = $state(new Map<number, number>());
	let requested = $state(new Set<number>());
	let failedPosters = $state(new Set<number>());
	let input = $state<HTMLInputElement | null>(null);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		const q = query;
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => (debounced = q.trim()), 300);
		return () => clearTimeout(debounceTimer);
	});

	$effect(() => {
		if (!open) {
			query = "";
			debounced = "";
			qualityProfileName = "";
			selectedId = null;
			sheetExpanded = false;
			sessionAdds = new Map();
			requested = new Set();
			failedPosters = new Set();
			return;
		}
		lockScroll();
		// The field is the whole point of the screen — open with the keyboard up.
		tick().then(() => input?.focus());
		return unlockScroll;
	});

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: open,
	}));

	const libraryQuery = createQuery<PaginatedMovies | PaginatedTVShows>(() => ({
		queryKey: isMovie ? ["movies"] : ["series"],
		queryFn: () =>
			api(isMovie ? "/movies?page=1&limit=500" : "/series?page=1&limit=500"),
		enabled: open,
	}));

	// Own key rather than the modals' — the payload is normalised here, and two
	// shapes under one cache key would strand whichever consumer mounts second.
	const searchQuery = createQuery<Hit[]>(() => ({
		queryKey: ["lookup-search", kind, debounced],
		queryFn: async () => {
			if (isMovie) {
				const items = await api<TMDBMovieResult[]>(
					`/search/movie?q=${encodeURIComponent(debounced)}`,
				);
				return items.map((m) => ({
					id: m.tmdb_id,
					title: m.title,
					year: m.year,
					poster_url: m.poster_url,
					overview: m.overview,
					already_added: m.already_added,
					subtitle:
						m.original_title.trim() && m.original_title.trim() !== m.title.trim()
							? m.original_title
							: undefined,
				}));
			}
			const res = await api<SeriesLookupResultList>(
				`/series/lookup?query=${encodeURIComponent(debounced)}`,
			);
			return (res.items ?? []).map((s) => ({
				id: s.tvdb_id,
				title: s.title,
				year: s.year,
				poster_url: s.poster_url,
				overview: s.overview,
				already_added: s.already_added,
				subtitle: s.network,
			}));
		},
		enabled: open && debounced.length >= 2,
		staleTime: 60_000,
	}));

	const detailQuery = createQuery<LookupDetail>(() => ({
		queryKey: ["lookup-detail", kind, selectedId],
		queryFn: () =>
			api<LookupDetail>(
				isMovie ? `/search/movie/${selectedId}` : `/series/lookup/${selectedId}`,
			),
		enabled: open && selectedId !== null,
		staleTime: 5 * 60_000,
	}));

	const qc = useQueryClient();
	const addMutation = createMutation<Movie | TVShow | null, Error, Hit>(() => ({
		onMutate: (h) => {
			pendingId = h.id;
		},
		mutationFn: async (h) => {
			if (!canAdd) {
				await api("/requests", {
					method: "POST",
					body: {
						media_type: isMovie ? "movie" : "tvshow",
						media_id: h.id,
						title: h.title,
						// Preference, not a promise — the reviewer can override it.
						quality_profile: qualityProfileName || undefined,
					},
				});
				return null;
			}
			if (isMovie) {
				const body: AddMovieRequest = { tmdb_id: h.id };
				if (qualityProfileName !== "") body.quality_profile = qualityProfileName;
				return api<Movie>("/movies", { method: "POST", body });
			}
			// Monitoring preset stays at the server default here; the desktop
			// modal is where a per-add preset is worth the extra control.
			const body: AddSeriesRequest = { tvdb_id: h.id, preset: "all" };
			if (qualityProfileName !== "") body.quality_profile = qualityProfileName;
			return api<TVShow>("/series", { method: "POST", body });
		},
		onSuccess: (item, h) => {
			if (!canAdd || !item) {
				requested = new Set(requested).add(h.id);
				toast.ok(i18n.toast_requested({ title: h.title }));
			} else {
				sessionAdds = new Map(sessionAdds).set(h.id, item.id);
				qc.invalidateQueries({ queryKey: isMovie ? ["movies"] : ["series"] });
				qc.invalidateQueries({
					queryKey: isMovie ? ["movies", "counts"] : ["series", "counts"],
				});
				toast.ok(i18n.toast_added({ title: h.title }));
			}
			// Back to the grid — the badge carries the new state, and the next
			// title is one tap away.
			selectedId = null;
		},
		onError: (e) => toast.err(errorText(e, i18n.common_add_failed())),
		onSettled: () => {
			pendingId = null;
		},
	}));

	let results = $derived(searchQuery.data ?? []);
	let qpOptions = $derived([
		{ value: "", label: canAdd ? i18n.quality_server_default() : i18n.quality_no_preference() },
		...(qpQuery.data ?? []).map((p) => ({ value: p.name, label: p.name })),
	]);

	// Lookup id → the library row, so an already-owned title can show what you
	// have instead of offering to add it again.
	let libraryByLookupId = $derived.by(() => {
		const map = new Map<number, Movie | TVShow>();
		for (const item of libraryQuery.data?.items ?? []) {
			// `in` rather than `isMovie`: the flag is a prop, so it narrows nothing
			// for the compiler, and the required id field is the real discriminant.
			const key = "tmdb_id" in item ? item.tmdb_id : item.tvdb_id;
			if (key) map.set(key, item);
		}
		return map;
	});
	const localFor = (id: number) => libraryByLookupId.get(id);
	const localIdFor = (id: number) =>
		libraryByLookupId.get(id)?.id ?? sessionAdds.get(id);
	const isHeld = (h: Hit) =>
		localIdFor(h.id) !== undefined || !!h.already_added;

	let heldCount = $derived(results.filter(isHeld).length);
	let selected = $derived(results.find((r) => r.id === selectedId));
	let selectedLocal = $derived(selected ? localFor(selected.id) : undefined);
	// The library row narrowed to one kind, so the movie-only and series-only
	// fields below are reachable without asserting.
	let selectedMovie = $derived(
		selectedLocal && "tmdb_id" in selectedLocal ? selectedLocal : undefined,
	);
	let selectedShow = $derived(
		selectedLocal && "tvdb_id" in selectedLocal ? selectedLocal : undefined,
	);
	let selectedLocalId = $derived(selected ? localIdFor(selected.id) : undefined);
	let selectedHeld = $derived(selected ? isHeld(selected) : false);
	let selectedRequested = $derived(selected ? requested.has(selected.id) : false);
	let selectedPending = $derived(selected ? pendingId === selected.id : false);

	let panelItem = $derived(
		selected
			? {
					title: selected.title,
					year: selected.year,
					poster_url: failedPosters.has(selected.id)
						? undefined
						: selected.poster_url,
					overview: selected.overview,
					subtitle: selected.subtitle,
				}
			: undefined,
	);
	let synopsis = $derived(detailQuery.data?.overview ?? selected?.overview ?? "");
	let runtimeText = $derived.by(() => {
		const m = detailQuery.data?.runtime;
		if (!m || m <= 0) return "";
		return m < 60 ? `${m} min` : `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, "0")}m`;
	});
	let ratingText = $derived(
		detailQuery.data?.rating && detailQuery.data.rating > 0
			? detailQuery.data.rating.toFixed(1)
			: "",
	);

	// What you already hold, for the library state of the sheet.
	let heldSize = $derived.by(() => {
		if (!selectedMovie) return "";
		let total = 0;
		for (const f of selectedMovie.media_files ?? []) total += f.size;
		return total > 0 ? formatBytes(total, "") : "";
	});
	let heldDetail = $derived.by(() => {
		if (selectedMovie) {
			return [selectedMovie.quality_profile, heldSize].filter(Boolean).join(" · ");
		}
		if (!selectedShow) return "";
		const have = selectedShow.have_episodes ?? 0;
		const total = selectedShow.total_episodes ?? 0;
		return [
			selectedShow.quality_profile,
			total > 0 ? `${have}/${total} episodes` : "",
		]
			.filter(Boolean)
			.join(" · ");
	});

	function pick(h: Hit) {
		selectedId = h.id;
		sheetExpanded = false;
	}
	function markPosterFailed(id: number) {
		failedPosters = new Set(failedPosters).add(id);
	}
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 flex flex-col bg-bg-deep md:hidden"
		transition:fly={{ y: 28, duration: 200, easing: cubicOut }}
	>
		<header class="flex flex-none items-center justify-between gap-3 px-3 pt-3 pb-2">
			<button
				type="button"
				onclick={onClose}
				class="-ml-1 inline-flex items-center gap-0.5 rounded-md py-1 pr-2 pl-1 text-[15px] text-accent-text transition active:opacity-70"
			>
				<ChevronLeft size={20} aria-hidden="true" />
				{i18n.nav_library()}
			</button>
			<span class="font-mono text-[10.5px] uppercase tracking-[0.16em] text-fg-faint">
				{isMovie ? "TMDB" : "TVDB"}
			</span>
		</header>

		<div class="relative flex-none px-4">
			<Search
				class="pointer-events-none absolute left-7 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint"
				aria-hidden="true"
			/>
			<input
				type="search"
				bind:this={input}
				bind:value={query}
				placeholder={isMovie ? i18n.lookup_search_tmdb_placeholder() : i18n.lookup_search_tvdb_placeholder()}
				autocomplete="off"
				aria-label={isMovie ? i18n.lookup_search_tmdb() : i18n.lookup_search_tvdb()}
				class="h-11 w-full rounded-xl border border-border bg-bg-card pr-10 pl-10 text-base text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
			/>
			{#if query.length > 0}
				<button
					type="button"
					onclick={() => {
						query = "";
						input?.focus();
					}}
					aria-label={i18n.common_clear_search()}
					class="absolute right-6 top-1/2 grid h-7 w-7 -translate-y-1/2 place-items-center rounded-full bg-surface text-fg-subtle transition active:opacity-70"
				>
					<X size={13} aria-hidden="true" />
				</button>
			{/if}
		</div>

		{#if debounced.length >= 2 && !searchQuery.isLoading && !searchQuery.isError && results.length > 0}
			<p class="flex-none px-5 pt-3 pb-1 text-[11.5px] text-fg-faint">
				{results.length}
				{results.length === 1 ? "match" : "matches"}{heldCount > 0
					? ` · ${heldCount} already in your library`
					: ""}
			</p>
		{/if}

		<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pt-2 pb-8">
			{#if debounced.length < 2}
				<div class="flex flex-col items-center justify-center px-8 py-20 text-center">
					<Search class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">
						Search {isMovie ? "TMDB" : "TVDB"}
					</p>
					<p class="mt-1 text-xs text-fg-faint">
						Type at least 2 characters to find a {isMovie ? "movie" : "series"}.
					</p>
				</div>
			{:else if searchQuery.isLoading}
				<div class="grid grid-cols-3 gap-3">
					{#each [0, 1, 2, 3, 4, 5] as i (i)}
						<div>
							<div class="aspect-[2/3] animate-pulse rounded-lg bg-bg-card"></div>
							<div class="mt-2 h-2.5 w-3/4 animate-pulse rounded bg-bg-card"></div>
						</div>
					{/each}
				</div>
			{:else if searchQuery.isError}
				<p
					role="alert"
					class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-10 text-center text-xs text-status-failed"
				>
					{errorText(searchQuery.error, i18n.common_search_failed())}
				</p>
			{:else if results.length === 0}
				<div class="flex flex-col items-center justify-center px-8 py-20 text-center">
					{#if isMovie}
						<Film class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					{:else}
						<Tv class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					{/if}
					<p class="text-sm font-medium text-fg-muted">{i18n.common_no_matches()}</p>
					<p class="mt-1 text-xs text-fg-faint">
						Nothing on {isMovie ? "TMDB" : "TVDB"} for &ldquo;{debounced}&rdquo;.
					</p>
				</div>
			{:else}
				<ul class="grid grid-cols-3 gap-3">
					{#each results as r (r.id)}
						{@const held = isHeld(r)}
						{@const asked = requested.has(r.id)}
						<li>
							<button
								type="button"
								onclick={() => pick(r)}
								class="w-full text-left transition active:opacity-80"
							>
								<div
									class="relative aspect-[2/3] overflow-hidden rounded-lg border border-white/[0.06] bg-bg-card shadow-1"
								>
									<div class="absolute inset-0 grid place-items-center text-fg-faint">
										{#if isMovie}
											<Film class="h-6 w-6" aria-hidden="true" />
										{:else}
											<Tv class="h-6 w-6" aria-hidden="true" />
										{/if}
									</div>
									{#if r.poster_url && !failedPosters.has(r.id)}
										<img
											src={r.poster_url}
											alt=""
											loading="lazy"
											onerror={() => markPosterFailed(r.id)}
											class="relative h-full w-full object-cover"
										/>
									{/if}
									<span
										aria-hidden="true"
										class="absolute right-1.5 bottom-1.5 grid h-7 w-7 place-items-center rounded-full shadow-2 {held ||
										asked
											? 'border border-accent-line bg-black/70 text-accent-text'
											: 'bg-accent text-fg-on-accent'}"
									>
										{#if held || asked}
											<Check size={14} aria-hidden="true" />
										{:else}
											<Plus size={16} aria-hidden="true" />
										{/if}
									</span>
								</div>
								<p class="mt-1.5 truncate text-[12.5px] font-medium text-fg">
									{r.title}
								</p>
								<p class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
									{r.year ?? "—"}
								</p>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</div>

	<LookupSheet
		open={selected !== undefined}
		bind:expanded={sheetExpanded}
		label={isMovie ? i18n.lookup_movie_details() : i18n.lookup_series_details()}
		onClose={() => (selectedId = null)}
	>
		{#snippet peek(atFullHeight)}
			{#if selected}
				<div class="flex gap-4 pt-1">
					<div
						class="relative aspect-[2/3] w-[84px] flex-none overflow-hidden rounded-lg border border-white/[0.06] bg-bg-card shadow-2"
					>
						<div class="absolute inset-0 grid place-items-center text-fg-faint">
							{#if isMovie}
								<Film class="h-6 w-6" aria-hidden="true" />
							{:else}
								<Tv class="h-6 w-6" aria-hidden="true" />
							{/if}
						</div>
						{#if selected.poster_url && !failedPosters.has(selected.id)}
							<img
								src={selected.poster_url}
								alt=""
								class="relative h-full w-full object-cover"
							/>
						{/if}
					</div>
					<div class="flex min-w-0 flex-1 flex-col justify-center">
						{#if selectedHeld}
							<span
								class="mb-1.5 font-mono text-[10px] uppercase tracking-[0.14em] text-accent-text"
							>
								{i18n.status_in_library()}
							</span>
						{/if}
						<h2 class="text-[20px] font-bold leading-tight tracking-tight text-fg">
							{selected.title}
						</h2>
						{#if selected.subtitle}
							<p class="mt-1 truncate text-[12.5px] italic text-fg-faint">
								{selected.subtitle}
							</p>
						{/if}
						<div class="mt-2.5 flex flex-wrap gap-1.5">
							{#if ratingText}
								<span
									class="inline-flex h-6 items-center gap-1 rounded-full border border-status-wanted/30 bg-surface px-2.5 font-mono text-[11px] text-status-wanted"
								>
									<Star size={11} aria-hidden="true" />
									{ratingText}
								</span>
							{/if}
							{#if runtimeText}
								<span
									class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
								>
									{runtimeText}
								</span>
							{/if}
							{#if selected.year}
								<span
									class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted"
								>
									{selected.year}
								</span>
							{/if}
						</div>
					</div>
				</div>
				{#if synopsis}
					<section class="mt-4">
						<h3
							class="mb-2 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint"
						>
							{i18n.detail_synopsis()}
						</h3>
						<p
							class="text-[13.5px] leading-relaxed text-fg-muted [text-wrap:pretty] {atFullHeight
								? ''
								: 'line-clamp-3'}"
						>
							{synopsis}
						</p>
					</section>
				{/if}
			{/if}
		{/snippet}

		{#snippet full()}
			<div class="pb-4">
				<LookupDetailPanel
					{kind}
					item={panelItem}
					detail={detailQuery.data}
					loading={detailQuery.isLoading}
					error={detailQuery.isError
						? (errorText(detailQuery.error, i18n.torrent_details_failed()))
						: undefined}
					compact
					headless
				/>
			</div>
		{/snippet}

		{#snippet footer()}
			{#if selectedHeld}
				<div
					class="flex h-[46px] items-center justify-between gap-3 rounded-xl border border-border bg-surface px-3.5"
				>
					{#if selectedMovie}
						<StatusPill status={movieStatus(selectedMovie)} size="sm" />
					{:else}
						<span class="text-[13.5px] text-fg">{i18n.status_in_library()}</span>
					{/if}
					<span class="truncate font-mono text-[12px] text-fg-muted">
						{heldDetail}
					</span>
				</div>
				{#if selectedLocalId !== undefined}
					<a
						href={`/${isMovie ? "movies" : "series"}/${selectedLocalId}`}
						onclick={onClose}
						class="mt-2.5 flex h-[50px] items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[16px] font-semibold text-fg-muted transition active:opacity-80"
					>
						{i18n.action_open_in_library()}
						<ArrowUpRight size={17} aria-hidden="true" />
					</a>
				{/if}
			{:else if selectedRequested}
				<div
					class="flex h-[50px] items-center justify-center gap-2 rounded-xl border border-accent-line bg-accent-soft text-[16px] font-semibold text-accent-text"
				>
					<Check size={17} aria-hidden="true" />
					{i18n.status_requested()}
				</div>
			{:else}
				<div
					class="flex h-[46px] items-center justify-between gap-3 rounded-xl border border-border bg-surface pr-1.5 pl-3.5"
				>
					<span class="flex-none text-[13.5px] text-fg">
						{canAdd ? i18n.quality_profile() : i18n.quality_preferred()}
					</span>
					<div class="w-[9.5rem]">
						<Select
							value={qualityProfileName}
							options={qpOptions}
							onChange={(v) => (qualityProfileName = v)}
							ariaLabel={canAdd ? i18n.quality_profile() : i18n.quality_preferred()}
						/>
					</div>
				</div>
				<button
					type="button"
					disabled={selectedPending}
					aria-busy={selectedPending}
					onclick={() => selected && addMutation.mutate(selected)}
					class="mt-2.5 flex h-[50px] w-full items-center justify-center gap-2 rounded-xl bg-accent text-[16px] font-semibold text-fg-on-accent transition active:opacity-80 disabled:opacity-60"
				>
					{#if selectedPending}
						<LoaderCircle size={17} class="animate-spin" aria-hidden="true" />
						{canAdd ? i18n.action_adding() : i18n.action_requesting()}
					{:else}
						<Plus size={18} aria-hidden="true" />
						{canAdd
							? isMovie
								? i18n.action_add_movie()
								: i18n.action_add_series()
							: i18n.action_request()}
					{/if}
				</button>
			{/if}
		{/snippet}
	</LookupSheet>
{/if}
