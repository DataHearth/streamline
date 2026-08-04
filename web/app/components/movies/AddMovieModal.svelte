<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		Search,
		Plus,
		Film,
		ArrowUpRight,
		Check,
		X,
		Loader2,
		Gauge,
	} from "@lucide/svelte";
	import { fade } from "svelte/transition";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { auth } from "../../lib/auth.svelte";
	import type {
		AddMovieRequest,
		LookupDetail,
		Movie,
		PaginatedMovies,
		QualityProfile,
		TMDBMovieResult,
	} from "../../lib/types";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import LookupDetailPanel from "../shared/LookupDetailPanel.svelte";

	type Props = {
		open: boolean;
		onClose: () => void;
		// "pick" turns the modal into a TMDB selector: no quality profile, no
		// library add — confirming a result calls onPick with the chosen result
		// and leaves the close/side-effects to the caller.
		mode?: "add" | "pick";
		seedQuery?: string;
		onPick?: (result: TMDBMovieResult) => void;
	};
	let {
		open,
		onClose,
		mode = "add",
		seedQuery = "",
		onPick,
	}: Props = $props();

	// request_only users create a request instead of adding directly (add mode
	// only); admins and members add to the library.
	let canAdd = $derived(auth.canAddDirectly);

	let query = $state("");
	let debounced = $state("");
	// "" means "let the backend resolve the default profile".
	let qualityProfileName = $state<string>("");
	// tmdb_id → local movie id for adds made during this modal session;
	// covers the gap between mutation success and the movies refetch.
	let sessionAdds = $state(new Map<number, number>());
	let pendingTmdbId = $state<number | null>(null);
	let failedPosters = $state(new Set<number>());
	let searchInput = $state<HTMLInputElement | null>(null);
	let resultsList = $state<HTMLUListElement | null>(null);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	// The highlighted result, whose detail fills the right-hand panel.
	let selectedTmdbId = $state<number | null>(null);
	// Below md the two columns don't fit side by side, so the modal shows one
	// at a time: results until you choose, then the panel with a back button.
	let showPanelOnNarrow = $state(false);

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
			sessionAdds = new Map();
			failedPosters = new Set();
			selectedTmdbId = null;
			showPanelOnNarrow = false;
			return;
		}
		// Seed pick-mode searches from the caller (e.g. the import file's
		// parsed title) so the candidates surface immediately, skipping the
		// debounce on first paint.
		if (mode === "pick" && seedQuery) {
			query = seedQuery;
			debounced = seedQuery.trim();
		}
	});

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: open && mode === "add",
	}));
	const moviesQuery = createQuery<PaginatedMovies>(() => ({
		queryKey: ["movies"],
		queryFn: () => api<PaginatedMovies>("/movies?page=1&limit=500"),
		enabled: open && mode === "add",
	}));

	const searchQuery = createQuery<TMDBMovieResult[]>(() => ({
		queryKey: ["tmdb-search", debounced],
		queryFn: () =>
			api<TMDBMovieResult[]>(
				`/search/movie?q=${encodeURIComponent(debounced)}`,
			),
		enabled: open && debounced.length >= 2,
		staleTime: 60_000,
	}));

	// Lazily fetched, one call per highlighted title — the search response
	// itself stays as small as it is.
	const detailQuery = createQuery<LookupDetail>(() => ({
		queryKey: ["tmdb-detail", selectedTmdbId],
		queryFn: () => api<LookupDetail>(`/search/movie/${selectedTmdbId}`),
		enabled: open && selectedTmdbId !== null,
		staleTime: 5 * 60_000,
	}));

	const qc = useQueryClient();
	const addMutation = createMutation<Movie | null, Error, TMDBMovieResult>(() => ({
		onMutate: (m) => {
			pendingTmdbId = m.tmdb_id;
		},
		mutationFn: async (m) => {
			if (!canAdd) {
				await api("/requests", {
					method: "POST",
					body: {
						media_type: "movie",
						media_id: m.tmdb_id,
						title: m.title,
						// Preference, not a promise — the reviewer can override it.
						quality_profile: qualityProfileName || undefined,
					},
				});
				return null;
			}
			const body: AddMovieRequest = { tmdb_id: m.tmdb_id };
			if (qualityProfileName !== "") {
				body.quality_profile = qualityProfileName;
			}
			return api<Movie>("/movies", { method: "POST", body });
		},
		onSuccess: (movie, m) => {
			if (!canAdd || !movie) {
				toast.ok(`Requested ${m.title}`);
				return;
			}
			sessionAdds = new Map(sessionAdds).set(m.tmdb_id, movie.id);
			qc.invalidateQueries({ queryKey: ["movies"] });
			qc.invalidateQueries({ queryKey: ["movies", "counts"] });
			toast.ok(`Added ${m.title}`);
		},
		onError: (e) => toast.err(e.message ?? "Add failed"),
		onSettled: () => {
			pendingTmdbId = null;
		},
	}));

	let results = $derived(searchQuery.data ?? []);
	let qpItems = $derived(qpQuery.data ?? []);
	let qpOptions = $derived<{ value: string; label: string }[]>([
		{ value: "", label: canAdd ? "Server default" : "No preference" },
		...qpItems.map((p) => ({ value: p.name, label: p.name })),
	]);
	let qpSelected = $derived(qualityProfileName);
	function onQpChange(v: string) {
		qualityProfileName = v;
	}
	let libraryByTmdb = $derived.by(() => {
		const map = new Map<number, number>();
		for (const m of moviesQuery.data?.items ?? []) {
			map.set(m.tmdb_id, m.id);
		}
		return map;
	});
	const resolveLocalId = (tmdbId: number): number | undefined =>
		libraryByTmdb.get(tmdbId) ?? sessionAdds.get(tmdbId);

	// Keep the highlight on a row that still exists as results change.
	$effect(() => {
		const list = results;
		if (list.length === 0) {
			if (selectedTmdbId !== null) selectedTmdbId = null;
			return;
		}
		if (!list.some((r) => r.tmdb_id === selectedTmdbId)) {
			selectedTmdbId = list[0].tmdb_id;
		}
	});

	let selected = $derived(results.find((r) => r.tmdb_id === selectedTmdbId));
	let selectedLocalId = $derived(
		selected ? resolveLocalId(selected.tmdb_id) : undefined,
	);
	let selectedInLibrary = $derived(
		selected ? selectedLocalId !== undefined || !!selected.already_added : false,
	);
	let selectedPending = $derived(
		selected ? pendingTmdbId === selected.tmdb_id : false,
	);
	let panelItem = $derived(
		selected
			? {
					title: selected.title,
					year: selected.year,
					poster_url: failedPosters.has(selected.tmdb_id)
						? undefined
						: selected.poster_url,
					overview: selected.overview,
					subtitle:
						selected.original_title.trim() &&
						selected.original_title.trim() !== selected.title.trim()
							? selected.original_title
							: undefined,
				}
			: undefined,
	);

	let announcer = $derived.by(() => {
		if (debounced.length < 2) return "";
		if (searchQuery.isLoading) return "Searching TMDB";
		if (searchQuery.isError)
			return searchQuery.error?.message ?? "Search failed";
		if (results.length === 0) return `No results for "${debounced}"`;
		const n = results.length;
		return `${n} result${n === 1 ? "" : "s"} for "${debounced}"`;
	});

	function selectResult(r: TMDBMovieResult, revealPanel = false) {
		selectedTmdbId = r.tmdb_id;
		if (revealPanel) showPanelOnNarrow = true;
	}

	function rowButtons(): HTMLElement[] {
		if (!resultsList) return [];
		return Array.from(
			resultsList.querySelectorAll<HTMLElement>("button[data-row]"),
		);
	}

	function onSearchKeydown(e: KeyboardEvent) {
		if (e.key === "ArrowDown" && results.length > 0) {
			e.preventDefault();
			rowButtons()[0]?.focus();
		}
	}

	// Arrow keys walk the list and move the highlight with focus, so the panel
	// tracks whatever row you're on without a click.
	function onResultsKeydown(e: KeyboardEvent) {
		if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
		const rows = rowButtons();
		const idx = rows.indexOf(document.activeElement as HTMLElement);
		if (idx < 0) return;
		e.preventDefault();
		if (e.key === "ArrowUp" && idx === 0) {
			searchInput?.focus();
			return;
		}
		const next =
			e.key === "ArrowDown" ? Math.min(idx + 1, rows.length - 1) : idx - 1;
		if (results[next]) selectResult(results[next]);
		rows[next]?.focus();
	}

	// focus doesn't bubble; focusin does. Tabbing into a row highlights it too.
	function onResultsFocusIn(e: FocusEvent) {
		const btn = (e.target as HTMLElement | null)?.closest?.(
			"button[data-row]",
		);
		if (!btn) return;
		const idx = rowButtons().indexOf(btn as HTMLElement);
		if (idx >= 0 && results[idx]) selectResult(results[idx]);
	}

	function markPosterFailed(tmdbId: number) {
		const next = new Set(failedPosters);
		next.add(tmdbId);
		failedPosters = next;
	}
</script>

<Modal
	{open}
	{onClose}
	title={mode === "pick"
		? "Choose match"
		: canAdd
			? "Add movie"
			: "Request a movie"}
	size="3xl"
	footer={results.length > 0 ? actionFooter : undefined}
>
	<div
		class="-mx-5 -my-4 grid min-h-[26rem] md:h-[60vh] md:max-h-[34rem] md:grid-cols-[340px_1fr]"
	>
		<div
			class="min-h-0 flex-col gap-3 border-border p-4 md:flex md:border-r {showPanelOnNarrow
				? 'hidden'
				: 'flex'}"
		>
			<div class="relative">
				<Search
					class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint"
					aria-hidden="true"
				/>
				<input
					type="search"
					bind:this={searchInput}
					bind:value={query}
					onkeydown={onSearchKeydown}
					placeholder="Search TMDB by title…"
					autocomplete="off"
					aria-label="Search TMDB by title"
					class="w-full rounded-md border border-border bg-bg-card py-2 pl-10 pr-10 text-sm text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
				/>
				{#if query.length > 0}
					<button
						type="button"
						onclick={() => {
							query = "";
							searchInput?.focus();
						}}
						aria-label="Clear search"
						class="absolute right-2 top-1/2 grid h-7 w-7 -translate-y-1/2 place-items-center rounded text-fg-faint transition hover:bg-surface hover:text-fg"
					>
						<X size={14} aria-hidden="true" />
					</button>
				{/if}
			</div>

			{#if debounced.length >= 2 && !searchQuery.isLoading && !searchQuery.isError && results.length > 0}
				<p class="px-1 text-[11px] text-fg-faint">
					{results.length}
					{results.length === 1 ? "match" : "matches"} for &ldquo;{debounced}&rdquo;
				</p>
			{/if}

			<span class="sr-only" aria-live="polite" aria-atomic="true"
				>{announcer}</span
			>

			<div class="flex min-h-0 flex-1 flex-col md:overflow-y-auto md:overscroll-contain">
			{#if debounced.length < 2}
				<div
					class="flex flex-1 flex-col items-center justify-center py-12 text-center"
				>
					<Search class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">Search TMDB</p>
					<p class="mt-1 text-xs text-fg-faint">
						Type at least 2 characters to find a movie.
					</p>
				</div>
			{:else if searchQuery.isLoading}
				<ul class="space-y-2">
					{#each [0, 1, 2, 3] as i (i)}
						<li
							class="flex items-stretch gap-3 rounded-lg border border-border bg-bg-card p-2.5"
						>
							<div
								class="aspect-[2/3] w-11 flex-none animate-pulse rounded-md bg-bg-deep"
							></div>
							<div class="flex min-w-0 flex-1 flex-col justify-center gap-2">
								<div class="h-3 w-2/3 animate-pulse rounded bg-bg-deep"></div>
								<div class="h-2 w-1/2 animate-pulse rounded bg-bg-deep"></div>
							</div>
						</li>
					{/each}
				</ul>
			{:else if searchQuery.isError}
				<p
					role="alert"
					class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-8 text-center text-xs text-status-failed"
				>
					{searchQuery.error?.message ?? "Search failed"}
				</p>
			{:else if results.length === 0}
				<div
					class="flex flex-1 flex-col items-center justify-center py-12 text-center"
				>
					<Film class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">No matches</p>
					<p class="mt-1 text-xs text-fg-faint">
						Nothing on TMDB for &ldquo;{debounced}&rdquo;.
					</p>
				</div>
			{:else}
				<!-- Keydown delegates roving focus across the row buttons; the <ul> itself isn't interactive -->
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<ul
					bind:this={resultsList}
					onkeydown={onResultsKeydown}
					onfocusin={onResultsFocusIn}
					class="space-y-2"
				>
					{#each results as r (r.tmdb_id)}
						{@const localId = resolveLocalId(r.tmdb_id)}
						{@const inLibrary = localId !== undefined || r.already_added}
						{@const isSel = r.tmdb_id === selectedTmdbId}
						{@const showPoster = r.poster_url && !failedPosters.has(r.tmdb_id)}
						<li in:fade={{ duration: 140 }}>
							<button
								type="button"
								data-row
								aria-pressed={isSel}
								onclick={() => selectResult(r, true)}
								class="flex w-full items-stretch gap-3 rounded-lg border p-2.5 text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {isSel
									? 'border-accent-line bg-accent-soft'
									: inLibrary
										? 'border-accent/40 bg-accent/5 hover:border-border-strong'
										: 'border-border bg-bg-card hover:border-border-strong hover:bg-bg-elevated'}"
							>
								<div
									class="relative aspect-[2/3] w-11 flex-none overflow-hidden rounded-md border border-white/[0.06] bg-bg-deep shadow-1"
								>
									<div
										class="absolute inset-0 grid place-items-center text-fg-faint"
									>
										<Film class="h-4 w-4" aria-hidden="true" />
									</div>
									{#if showPoster}
										<img
											src={r.poster_url}
											alt=""
											loading="lazy"
											onerror={() => markPosterFailed(r.tmdb_id)}
											class="relative h-full w-full object-cover"
										/>
									{/if}
								</div>

								<div class="flex min-w-0 flex-1 flex-col justify-center">
									<span class="block truncate text-[13.5px] font-semibold text-fg">
										{r.title}
										{#if r.year}
											<span
												class="ml-1 font-mono text-[11px] font-normal text-fg-subtle"
												>· {r.year}</span
											>
										{/if}
									</span>
									{#if inLibrary}
										<span
											class="mt-1 inline-flex w-fit items-center rounded-full border border-border bg-bg-elevated px-2 py-0.5 font-mono text-[10px] uppercase tracking-[0.1em] text-fg-subtle"
										>
											In library
										</span>
									{:else if r.overview}
										<span
											class="mt-1 line-clamp-2 text-[11.5px] leading-snug text-fg-muted"
										>
											{r.overview}
										</span>
									{/if}
								</div>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
			</div>
		</div>

		<div
			class="min-h-0 md:overflow-y-auto md:overscroll-contain {showPanelOnNarrow
				? 'block'
				: 'hidden md:block'}"
		>
			<LookupDetailPanel
				kind="movie"
				item={panelItem}
				detail={detailQuery.data}
				loading={detailQuery.isLoading}
				error={detailQuery.isError
					? (detailQuery.error?.message ?? "Couldn't load details")
					: undefined}
				onBack={() => (showPanelOnNarrow = false)}
			/>
		</div>
	</div>
</Modal>

{#snippet actionFooter()}
	{#if mode === "add"}
		<div class="mr-auto flex items-center gap-2">
			<label
				for="add-movie-qp"
				class="inline-flex shrink-0 items-center gap-1.5 text-sm font-medium text-fg"
			>
				<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
				{canAdd ? "Quality profile" : "Preferred quality"}
			</label>
			<div class="w-48">
				<Select
					id="add-movie-qp"
					value={qpSelected}
					options={qpOptions}
					onChange={onQpChange}
				/>
			</div>
		</div>
	{/if}

	{#if selected}
		{#if mode === "pick"}
			<button
				type="button"
				onclick={() => selected && onPick?.(selected)}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
			>
				<Check size={15} aria-hidden="true" />
				Use match
			</button>
		{:else if selectedInLibrary && selectedLocalId !== undefined}
			<a
				href={`/movies/${selectedLocalId}`}
				onclick={onClose}
				class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-4 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
			>
				Open in library
				<ArrowUpRight size={15} aria-hidden="true" />
			</a>
		{:else if selectedInLibrary}
			<span
				class="inline-flex h-9 items-center rounded-md border border-border bg-bg-elevated px-4 text-sm font-medium text-fg-muted"
			>
				In library
			</span>
		{:else}
			<button
				type="button"
				disabled={selectedPending}
				aria-busy={selectedPending}
				onclick={() => selected && addMutation.mutate(selected)}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if selectedPending}
					<Loader2 size={15} class="animate-spin" aria-hidden="true" />
					{canAdd ? "Adding…" : "Requesting…"}
				{:else}
					<Plus size={15} aria-hidden="true" />
					{canAdd ? "Add movie" : "Request"}
				{/if}
			</button>
		{/if}
	{/if}
{/snippet}
