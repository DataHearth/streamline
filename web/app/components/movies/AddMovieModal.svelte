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
	import { fade, scale } from "svelte/transition";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { auth } from "../../lib/auth.svelte";
	import type {
		AddMovieRequest,
		Movie,
		PaginatedMovies,
		QualityProfile,
		TMDBMovieResult,
	} from "../../lib/types";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";

	type Props = {
		open: boolean;
		onClose: () => void;
		// "pick" turns the modal into a TMDB selector: no quality profile, no
		// library add — clicking a result calls onPick with the chosen result
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
			sessionAdds = new Map();
			failedPosters = new Set();
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
		{ value: "", label: "Server default" },
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

	let announcer = $derived.by(() => {
		if (debounced.length < 2) return "";
		if (searchQuery.isLoading) return "Searching TMDB";
		if (searchQuery.isError)
			return searchQuery.error?.message ?? "Search failed";
		if (results.length === 0) return `No results for "${debounced}"`;
		const n = results.length;
		return `${n} result${n === 1 ? "" : "s"} for "${debounced}"`;
	});

	function focusableRowControls(): HTMLElement[] {
		if (!resultsList) return [];
		return Array.from(
			resultsList.querySelectorAll<HTMLElement>(
				"button:not(:disabled), a",
			),
		);
	}

	function onSearchKeydown(e: KeyboardEvent) {
		if (e.key === "ArrowDown" && results.length > 0) {
			e.preventDefault();
			focusableRowControls()[0]?.focus();
		}
	}

	function onResultsKeydown(e: KeyboardEvent) {
		if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
		const controls = focusableRowControls();
		const idx = controls.indexOf(document.activeElement as HTMLElement);
		if (idx < 0) return;
		e.preventDefault();
		if (e.key === "ArrowDown") {
			controls[Math.min(idx + 1, controls.length - 1)]?.focus();
		} else if (idx === 0) {
			searchInput?.focus();
		} else {
			controls[idx - 1]?.focus();
		}
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
	size="xl"
	footer={mode === "add" && canAdd ? qpFooter : undefined}
>
	<div class="space-y-3">
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

		<div class="min-h-[18rem]">
			{#if debounced.length < 2}
				<div
					class="flex flex-col items-center justify-center py-12 text-center"
				>
					<Search
						class="mb-3 h-8 w-8 text-fg-faint"
						aria-hidden="true"
					/>
					<p class="text-sm font-medium text-fg-muted">
						Search TMDB
					</p>
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
								class="aspect-[2/3] w-12 flex-none animate-pulse rounded-md bg-bg-deep"
							></div>
							<div
								class="flex min-w-0 flex-1 flex-col justify-center gap-2"
							>
								<div
									class="h-3 w-2/3 animate-pulse rounded bg-bg-deep"
								></div>
								<div
									class="h-2 w-full animate-pulse rounded bg-bg-deep"
								></div>
								<div
									class="h-2 w-5/6 animate-pulse rounded bg-bg-deep"
								></div>
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
					class="flex flex-col items-center justify-center py-12 text-center"
				>
					<Film
						class="mb-3 h-8 w-8 text-fg-faint"
						aria-hidden="true"
					/>
					<p class="text-sm font-medium text-fg-muted">
						No matches
					</p>
					<p class="mt-1 text-xs text-fg-faint">
						Nothing on TMDB for &ldquo;{debounced}&rdquo;.
					</p>
				</div>
			{:else}
				<!-- Keydown delegates roving focus across the row controls (buttons/links); the <ul> itself isn't interactive -->
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<ul
					bind:this={resultsList}
					onkeydown={onResultsKeydown}
					class="space-y-2"
				>
					{#each results as r (r.tmdb_id)}
						{@const localId = resolveLocalId(r.tmdb_id)}
						{@const inLibrary = localId !== undefined}
						{@const pending = pendingTmdbId === r.tmdb_id}
						{@const showPoster =
							r.poster_url && !failedPosters.has(r.tmdb_id)}
						<li
							in:fade={{ duration: 140 }}
							class="group flex items-stretch gap-3 rounded-lg border bg-bg-card p-2.5 transition {inLibrary
								? 'border-accent/40 bg-accent/5'
								: 'border-border hover:border-border-strong hover:bg-bg-elevated'}"
						>
							<div
								class="relative aspect-[2/3] w-12 flex-none overflow-hidden rounded-md border border-white/[0.06] bg-bg-deep shadow-1"
							>
								<div
									class="absolute inset-0 grid place-items-center text-fg-faint"
								>
									<Film
										class="h-5 w-5"
										aria-hidden="true"
									/>
								</div>
								{#if showPoster}
									<img
										src={r.poster_url}
										alt=""
										loading="lazy"
										onerror={() =>
											markPosterFailed(r.tmdb_id)}
										class="relative h-full w-full object-cover"
									/>
								{/if}
							</div>

							<div
								class="flex min-w-0 flex-1 flex-col justify-center"
							>
								<p class="truncate text-sm font-semibold text-fg">
									{r.title}
									{#if r.year}
										<span
											class="ml-1 font-mono text-[11px] font-normal text-fg-subtle"
											>· {r.year}</span
										>
									{/if}
								</p>
								{#if r.original_title.trim() && r.original_title.trim() !== r.title.trim()}
									<p
										class="truncate text-xs italic text-fg-faint"
									>
										{r.original_title}
									</p>
								{/if}
								{#if r.overview}
									<p
										class="mt-1 line-clamp-2 text-xs leading-snug text-fg-muted"
									>
										{r.overview}
									</p>
								{/if}
							</div>

							<div class="flex flex-none items-center">
								{#if mode === "pick"}
									<button
										type="button"
										onclick={() => onPick?.(r)}
										class="inline-flex h-8 items-center gap-1 rounded-md bg-accent px-3 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover"
									>
										<Check size={14} aria-hidden="true" />
										Use match
									</button>
								{:else if inLibrary}
									<a
										in:scale={{
											duration: 220,
											start: 0.85,
										}}
										href={`/movies/${localId}`}
										onclick={onClose}
										class="inline-flex h-8 items-center gap-1 rounded-md border border-border bg-bg-elevated px-3 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
									>
										In library
										<ArrowUpRight
											size={14}
											aria-hidden="true"
										/>
									</a>
								{:else}
									<button
										type="button"
										disabled={pending}
										aria-busy={pending}
										onclick={() =>
											addMutation.mutate(r)}
										class="inline-flex h-8 items-center gap-1 rounded-md bg-accent px-3 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
									>
										{#if pending}
											<Loader2
												size={14}
												class="animate-spin"
												aria-hidden="true"
											/>
											{canAdd ? "Adding…" : "Requesting…"}
										{:else}
											<Plus
												size={14}
												aria-hidden="true"
											/>
											{canAdd ? "Add" : "Request"}
										{/if}
									</button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</div>
</Modal>

{#snippet qpFooter()}
	<label
		for="add-movie-qp"
		class="mr-auto inline-flex items-center gap-1.5 text-sm font-medium text-fg"
	>
		<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
		Quality profile
	</label>
	<div class="w-60">
		<Select
			id="add-movie-qp"
			value={qpSelected}
			options={qpOptions}
			onChange={onQpChange}
		/>
	</div>
{/snippet}
