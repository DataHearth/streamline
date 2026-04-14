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
	import { fade, scale } from "svelte/transition";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { auth } from "../../lib/auth.svelte";
	import type {
		AddSeriesRequest,
		MonitoringPreset,
		PaginatedTVShows,
		QualityProfile,
		SeriesLookupResult,
		SeriesLookupResultList,
		TVShow,
	} from "../../lib/types";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";

	type Props = {
		open: boolean;
		onClose: () => void;
		// "pick" turns the modal into a TVDB selector: no quality/monitor config,
		// no library add — clicking a result calls onPick with the chosen result
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
	let failedPosters = $state(new Set<number>());
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

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
			sessionAdds = new Map();
			failedPosters = new Set();
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
		{ value: "", label: "Server default" },
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

	let announcer = $derived.by(() => {
		if (debounced.length < 2) return "";
		if (searchQuery.isLoading) return "Searching TVDB";
		if (searchQuery.isError) return searchQuery.error?.message ?? "Search failed";
		if (results.length === 0) return `No results for "${debounced}"`;
		const n = results.length;
		return `${n} result${n === 1 ? "" : "s"} for "${debounced}"`;
	});
</script>

<Modal
	{open}
	{onClose}
	title={mode === "pick"
		? "Choose match"
		: canAdd
			? "Add series"
			: "Request a series"}
	size="xl"
	footer={mode === "add" && canAdd ? configFooter : undefined}
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

		<span class="sr-only" aria-live="polite" aria-atomic="true">{announcer}</span>

		<div class="min-h-[18rem]">
			{#if debounced.length < 2}
				<div class="flex flex-col items-center justify-center py-12 text-center">
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
								class="aspect-[2/3] w-12 flex-none animate-pulse rounded-md bg-bg-deep"
							></div>
							<div class="flex min-w-0 flex-1 flex-col justify-center gap-2">
								<div class="h-3 w-2/3 animate-pulse rounded bg-bg-deep"></div>
								<div class="h-2 w-full animate-pulse rounded bg-bg-deep"></div>
								<div class="h-2 w-5/6 animate-pulse rounded bg-bg-deep"></div>
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
				<div class="flex flex-col items-center justify-center py-12 text-center">
					<Tv class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">No matches</p>
					<p class="mt-1 text-xs text-fg-faint">
						Nothing on TVDB for &ldquo;{debounced}&rdquo;.
					</p>
				</div>
			{:else}
				<ul class="space-y-2">
					{#each results as r (r.tvdb_id)}
						{@const localId = resolveLocalId(r.tvdb_id)}
						{@const inLibrary = localId !== undefined || r.already_added}
						{@const pending = pendingTvdbId === r.tvdb_id}
						{@const showPoster = r.poster_url && !failedPosters.has(r.tvdb_id)}
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
									<Tv class="h-5 w-5" aria-hidden="true" />
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
								<p class="truncate text-sm font-semibold text-fg">
									{r.title}
									{#if r.year}
										<span
											class="ml-1 font-mono text-[11px] font-normal text-fg-subtle"
											>· {r.year}</span
										>
									{/if}
								</p>
								{#if r.network}
									<p class="truncate text-xs text-fg-subtle">{r.network}</p>
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
								{:else if inLibrary && localId !== undefined}
									<a
										in:scale={{ duration: 220, start: 0.85 }}
										href={`/series/${localId}`}
										onclick={onClose}
										class="inline-flex h-8 items-center gap-1 rounded-md border border-border bg-bg-elevated px-3 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
									>
										In library
										<ArrowUpRight size={14} aria-hidden="true" />
									</a>
								{:else if inLibrary}
									<span
										class="inline-flex h-8 items-center rounded-md border border-border bg-bg-elevated px-3 text-xs font-medium text-fg-muted"
									>
										In library
									</span>
								{:else}
									<button
										type="button"
										disabled={pending}
										aria-busy={pending}
										onclick={() => addMutation.mutate(r)}
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
											<Plus size={14} aria-hidden="true" />
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

{#snippet configFooter()}
	<div class="mr-auto flex flex-wrap items-center gap-x-5 gap-y-2">
		<div class="flex items-center gap-2">
			<label
				for="add-series-qp"
				class="inline-flex items-center gap-1.5 text-sm font-medium text-fg"
			>
				<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
				Quality
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
	</div>
{/snippet}
