<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		Search,
		Plus,
		Tv,
		ArrowUpRight,
		Check,
		Loader2,
		Gauge,
		Eye,
		X,
	} from "@lucide/svelte";
	import { fade } from "svelte/transition";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { auth } from "../../lib/auth.svelte";
	import type {
		AddSeriesRequest,
		LookupDetail,
		MonitoringPreset,
		PaginatedTVShows,
		QualityProfile,
		SeriesLookupResult,
		SeriesLookupResultList,
		TVShow,
	} from "../../lib/types";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import LookupDetailPanel from "../shared/LookupDetailPanel.svelte";

	type Props = {
		open: boolean;
		onClose: () => void;
		// "pick" turns the modal into a TVDB selector: no quality/monitor config,
		// no library add — confirming a result calls onPick with the chosen result
		// and leaves the close/side-effects to the caller.
		mode?: "add" | "pick";
		seedQuery?: string;
		onPick?: (result: SeriesLookupResult) => void;
	};
	let {
		open,
		onClose,
		mode = "add",
		seedQuery = "",
		onPick,
	}: Props = $props();

	// request_only users create a request instead of adding the show directly;
	// admins and members add it to the library.
	let canAdd = $derived(auth.canAddDirectly);

	let query = $state("");
	let debounced = $state("");
	// "" means "let the backend resolve the default profile".
	let qualityProfileName = $state<string>("");
	let preset = $state<MonitoringPreset>("all");
	// tvdb_id → local series id for adds made during this modal session;
	// covers the gap between mutation success and the series refetch.
	let sessionAdds = $state(new Map<number, number>());
	let pendingTvdbId = $state<number | null>(null);
	let searchInput = $state<HTMLInputElement | null>(null);
	let resultsList = $state<HTMLUListElement | null>(null);
	let failedPosters = $state(new Set<number>());
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	// The highlighted result, whose detail fills the right-hand panel.
	let selectedTvdbId = $state<number | null>(null);
	// Below md the two columns don't fit side by side, so the modal shows one
	// at a time: results until you choose, then the panel with a back button.
	let showPanelOnNarrow = $state(false);

	function markPosterFailed(tvdbId: number) {
		failedPosters = new Set(failedPosters).add(tvdbId);
	}

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
			preset = "all";
			sessionAdds = new Map();
			failedPosters = new Set();
			selectedTvdbId = null;
			showPanelOnNarrow = false;
			return;
		}
		// Seed pick-mode searches from the caller (e.g. the import folder's
		// parsed title) so candidates surface immediately, skipping the debounce.
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

	const seriesListQuery = createQuery<PaginatedTVShows>(() => ({
		queryKey: ["series"],
		queryFn: () => api<PaginatedTVShows>("/series?page=1&limit=500"),
		enabled: open && mode === "add",
	}));

	const searchQuery = createQuery<SeriesLookupResult[]>(() => ({
		queryKey: ["series-lookup", debounced],
		queryFn: async () => {
			const res = await api<SeriesLookupResultList>(
				`/series/lookup?query=${encodeURIComponent(debounced)}`,
			);
			return res.items ?? [];
		},
		enabled: open && debounced.length >= 2,
		staleTime: 60_000,
	}));

	// Lazily fetched, one call per highlighted title — the lookup response
	// itself stays as small as it is.
	const detailQuery = createQuery<LookupDetail>(() => ({
		queryKey: ["series-lookup-detail", selectedTvdbId],
		queryFn: () => api<LookupDetail>(`/series/lookup/${selectedTvdbId}`),
		enabled: open && selectedTvdbId !== null,
		staleTime: 5 * 60_000,
	}));

	const qc = useQueryClient();
	const addMutation = createMutation<TVShow | null, Error, SeriesLookupResult>(() => ({
		onMutate: (s) => {
			pendingTvdbId = s.tvdb_id;
		},
		mutationFn: async (s) => {
			if (!canAdd) {
				await api("/requests", {
					method: "POST",
					body: {
						media_type: "tvshow",
						media_id: s.tvdb_id,
						title: s.title,
						// Preference, not a promise — the reviewer can override it.
						quality_profile: qualityProfileName || undefined,
					},
				});
				return null;
			}
			const body: AddSeriesRequest = { tvdb_id: s.tvdb_id, preset };
			if (qualityProfileName !== "") {
				body.quality_profile = qualityProfileName;
			}
			return api<TVShow>("/series", { method: "POST", body });
		},
		onSuccess: (show, s) => {
			if (!canAdd || !show) {
				toast.ok(`Requested ${s.title}`);
				return;
			}
			sessionAdds = new Map(sessionAdds).set(s.tvdb_id, show.id);
			qc.invalidateQueries({ queryKey: ["series"] });
			qc.invalidateQueries({ queryKey: ["series", "counts"] });
			toast.ok(`Added ${s.title}`);
		},
		onError: (e) => toast.err(e.message ?? "Add failed"),
		onSettled: () => {
			pendingTvdbId = null;
		},
	}));

	let results = $derived(searchQuery.data ?? []);
	let qpItems = $derived(qpQuery.data ?? []);
	let qpOptions = $derived<{ value: string; label: string }[]>([
		{ value: "", label: canAdd ? "Server default" : "No preference" },
		...qpItems.map((p) => ({ value: p.name, label: p.name })),
	]);

	const presetOptions: { value: MonitoringPreset; label: string }[] = [
		{ value: "all", label: "All episodes" },
		{ value: "future", label: "Future episodes" },
		{ value: "missing", label: "Missing episodes" },
		{ value: "existing", label: "Existing episodes" },
		{ value: "pilot", label: "Pilot only" },
		{ value: "none", label: "None" },
	];

	let libraryByTvdb = $derived.by(() => {
		const map = new Map<number, number>();
		for (const s of seriesListQuery.data?.items ?? []) {
			map.set(s.tvdb_id, s.id);
		}
		return map;
	});
	const resolveLocalId = (tvdbId: number): number | undefined =>
		libraryByTvdb.get(tvdbId) ?? sessionAdds.get(tvdbId);

	// Keep the highlight on a row that still exists as results change.
	$effect(() => {
		const list = results;
		if (list.length === 0) {
			if (selectedTvdbId !== null) selectedTvdbId = null;
			return;
		}
		if (!list.some((r) => r.tvdb_id === selectedTvdbId)) {
			selectedTvdbId = list[0].tvdb_id;
		}
	});

	let selected = $derived(results.find((r) => r.tvdb_id === selectedTvdbId));
	let selectedLocalId = $derived(
		selected ? resolveLocalId(selected.tvdb_id) : undefined,
	);
	let selectedInLibrary = $derived(
		selected ? selectedLocalId !== undefined || !!selected.already_added : false,
	);
	let selectedPending = $derived(
		selected ? pendingTvdbId === selected.tvdb_id : false,
	);
	let panelItem = $derived(
		selected
			? {
					title: selected.title,
					year: selected.year,
					poster_url: failedPosters.has(selected.tvdb_id)
						? undefined
						: selected.poster_url,
					overview: selected.overview,
					subtitle: selected.network,
				}
			: undefined,
	);

	let announcer = $derived.by(() => {
		if (debounced.length < 2) return "";
		if (searchQuery.isLoading) return "Searching TVDB";
		if (searchQuery.isError) return searchQuery.error?.message ?? "Search failed";
		if (results.length === 0) return `No results for "${debounced}"`;
		const n = results.length;
		return `${n} result${n === 1 ? "" : "s"} for "${debounced}"`;
	});

	function selectResult(r: SeriesLookupResult, revealPanel = false) {
		selectedTvdbId = r.tvdb_id;
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
</script>

<Modal
	{open}
	{onClose}
	title={mode === "pick"
		? "Choose match"
		: canAdd
			? "Add series"
			: "Request a series"}
	size="3xl"
	footer={results.length > 0 ? actionFooter : undefined}
>
	<div class="-mx-5 -my-4 grid min-h-[26rem] md:grid-cols-[340px_1fr]">
		<div
			class="flex-col gap-3 border-border p-4 md:flex md:border-r {showPanelOnNarrow
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
					placeholder="Search TVDB by title…"
					autocomplete="off"
					aria-label="Search TVDB by title"
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

			<span class="sr-only" aria-live="polite" aria-atomic="true">{announcer}</span>

			{#if debounced.length < 2}
				<div
					class="flex flex-1 flex-col items-center justify-center py-12 text-center"
				>
					<Search class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">Search TVDB</p>
					<p class="mt-1 text-xs text-fg-faint">
						Type at least 2 characters to find a show.
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
					<Tv class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">No matches</p>
					<p class="mt-1 text-xs text-fg-faint">
						Nothing on TVDB for &ldquo;{debounced}&rdquo;.
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
					{#each results as r (r.tvdb_id)}
						{@const localId = resolveLocalId(r.tvdb_id)}
						{@const inLibrary = localId !== undefined || r.already_added}
						{@const isSel = r.tvdb_id === selectedTvdbId}
						{@const showPoster = r.poster_url && !failedPosters.has(r.tvdb_id)}
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
										<Tv class="h-4 w-4" aria-hidden="true" />
									</div>
									{#if showPoster}
										<img
											src={r.poster_url}
											alt=""
											loading="lazy"
											onerror={() => markPosterFailed(r.tvdb_id)}
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
									{:else if r.network}
										<span class="block truncate text-[11.5px] text-fg-subtle">
											{r.network}
										</span>
									{/if}
								</div>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>

		<div class={showPanelOnNarrow ? "block" : "hidden md:block"}>
			<LookupDetailPanel
				kind="series"
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
		<div class="mr-auto flex flex-wrap items-center gap-x-5 gap-y-2">
			<div class="flex items-center gap-2">
				<label
					for="add-series-qp"
					class="inline-flex shrink-0 items-center gap-1.5 text-sm font-medium text-fg"
				>
					<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
					{canAdd ? "Quality" : "Preferred quality"}
				</label>
				<div class="w-44">
					<Select
						id="add-series-qp"
						value={qualityProfileName}
						options={qpOptions}
						onChange={(v) => (qualityProfileName = v)}
					/>
				</div>
			</div>
			{#if canAdd}
				<div class="flex items-center gap-2">
					<label
						for="add-series-monitor"
						class="inline-flex items-center gap-1.5 text-sm font-medium text-fg"
					>
						<Eye size={16} class="text-fg-muted" aria-hidden="true" />
						Monitor
					</label>
					<div class="w-44">
						<Select
							id="add-series-monitor"
							value={preset}
							options={presetOptions}
							onChange={(v) => (preset = v)}
						/>
					</div>
				</div>
			{/if}
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
				href={`/series/${selectedLocalId}`}
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
					{canAdd ? "Add series" : "Request"}
				{/if}
			</button>
		{/if}
	{/if}
{/snippet}
