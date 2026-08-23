<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { goto, params } from "@roxi/routify";
	import { onMount } from "svelte";
	import {
		ArrowDown,
		ArrowLeft,
		ArrowUp,
		ArrowUpDown,
		Square,
		Trash2,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api, apiAllPages, errorText, type Paginated } from "../../../lib/api";
	import { cn } from "../../../lib/cn";
	import { formatDateTime, formatRelative } from "../../../lib/dates";
	import {
		commitNote,
		commitSummary,
		importModeLabel,
		importStatusMeta,
	} from "../../../lib/imports";
	import { toast } from "../../../lib/toast";
	import type {
		ImportFileClassification,
		ImportFileDecision,
		ImportScan,
		ImportScanFile,
		ImportScanShow,
		SeriesLookupResult,
		TMDBMovieResult,
	} from "../../../lib/types";
	import AddMovieModal from "../../../components/movies/AddMovieModal.svelte";
	import AddSeriesModal from "../../../components/series/AddSeriesModal.svelte";
	import Select from "../../../components/forms/Select.svelte";
	import Dialog from "../../../components/modals/Dialog.svelte";
	import DecisionStrip from "../../../components/library/DecisionStrip.svelte";
	import ImportFileRow from "../../../components/library/ImportFileRow.svelte";
	import ImportShowRow from "../../../components/library/ImportShowRow.svelte";
	import ImportProgress from "../../../components/library/ImportProgress.svelte";
	import ImportSteps from "../../../components/library/ImportSteps.svelte";
	import ImportTouchList from "../../../components/library/ImportTouchList.svelte";
	import ImportDecisionSheet from "../../../components/library/ImportDecisionSheet.svelte";
	import ImportCommitBar from "../../../components/library/ImportCommitBar.svelte";
	import ImportMatchSheet from "../../../components/library/ImportMatchSheet.svelte";
	import {
		fileEntry,
		showEntry,
		type TouchEntry,
	} from "../../../lib/imports-touch";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	let routeParams = $state<Record<string, string>>({});
	// goto is a derived store layered over the current fragment; calling
	// get(goto) inside a mutation callback can throw "derived() expects
	// stores as input" when the fragment is in flux (e.g. discard onSuccess
	// fires as the scan resource disappears). Snapshot the navigation
	// function once and call it from callbacks instead.
	let navigate: (path: string) => void = () => {};
	onMount(() => {
		const u1 = params.subscribe((p) => (routeParams = p));
		const u2 = goto.subscribe((fn) => (navigate = fn));
		return () => {
			u1();
			u2();
		};
	});

	const importId = $derived(Number(routeParams.id));

	let confirmCancel = $state(false);
	let confirmDiscard = $state(false);

	const qc = useQueryClient();

	const scanQuery = createQuery<ImportScan>(() => ({
		queryKey: ["import", importId],
		queryFn: () => api<ImportScan>(`/library/imports/${importId}`),
		enabled: Number.isFinite(importId) && importId > 0,
		refetchInterval: (q) => {
			const s = q.state.data?.status;
			return s === "running" || s === "committing" ? 1500 : false;
		},
	}));

	const scan = $derived(scanQuery.data);
	const isSeries = $derived(scan?.kind === "series");

	// Toast once when commit finishes — observed by watching the live→terminal
	// transition rather than threading a callback through the mutation.
	let prevStatus = $state<string | undefined>(undefined);
	$effect(() => {
		const cur = scan?.status;
		if (!cur) return;
		if (prevStatus === "committing" && cur === "completed") {
			toast.ok(
				i18n.imports_commit_finished({
					imported: scan.commit_success_count,
					failed: scan.commit_failed_count,
				}),
			);
		}
		prevStatus = cur;
	});

	let q = $state("");
	let debouncedQ = $state("");
	let classification = $state<ImportFileClassification | "">("");


	// q feeds the query key, so every keystroke would otherwise be its own cache
	// entry — a fresh request plus a drop back to the loading state per letter.
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const raw = q;
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => (debouncedQ = raw.trim()), 300);
		return () => clearTimeout(debounceTimer);
	});

	const filesQuery = createQuery<Paginated<ImportScanFile>>(() => ({
		queryKey: ["import", importId, "files", { q: debouncedQ, classification }],
		queryFn: () => {
			const sp = new URLSearchParams();
			if (debouncedQ) sp.set("q", debouncedQ);
			if (classification) sp.set("classification", classification);
			return apiAllPages<ImportScanFile>(
				`/library/imports/${importId}/files?${sp}`,
			);
		},
		enabled:
			Number.isFinite(importId) &&
			importId > 0 &&
			scan != null &&
			!isSeries &&
			scan.status !== "running" &&
			scan.status !== "committing",
	}));

	// Separate, unfiltered query that backs the DecisionStrip count + bulk
	// skip target. Stays in sync with the user's table filter only via cache
	// invalidation after mutations.
	const pendingQuery = createQuery<Paginated<ImportScanFile>>(() => ({
		queryKey: ["import", importId, "pending"],
		queryFn: () => {
			return apiAllPages<ImportScanFile>(
				`/library/imports/${importId}/files`,
			);
		},
		enabled:
			Number.isFinite(importId) &&
			importId > 0 &&
			!isSeries &&
			scan?.status === "awaiting_review",
	}));

	// Series scans render per-show rows instead of files. The two queries mirror
	// filesQuery / pendingQuery but hit the /shows endpoint.
	const showsQuery = createQuery<Paginated<ImportScanShow>>(() => ({
		queryKey: ["import", importId, "shows", { q: debouncedQ, classification }],
		queryFn: () => {
			const sp = new URLSearchParams();
			if (debouncedQ) sp.set("q", debouncedQ);
			if (classification) sp.set("classification", classification);
			return apiAllPages<ImportScanShow>(
				`/library/imports/${importId}/shows?${sp}`,
			);
		},
		enabled:
			Number.isFinite(importId) &&
			importId > 0 &&
			isSeries &&
			scan != null &&
			scan.status !== "running" &&
			scan.status !== "committing",
	}));

	const pendingShowsQuery = createQuery<Paginated<ImportScanShow>>(() => ({
		queryKey: ["import", importId, "pending-shows"],
		queryFn: () => {
			return apiAllPages<ImportScanShow>(
				`/library/imports/${importId}/shows`,
			);
		},
		enabled:
			Number.isFinite(importId) &&
			importId > 0 &&
			isSeries &&
			scan?.status === "awaiting_review",
	}));

	let pendingShows = $derived(
		(pendingShowsQuery.data?.items ?? []).filter(
			(sh) =>
				sh.decision === "pending" &&
				(sh.classification === "ambiguous" ||
					sh.classification === "unmatched"),
		),
	);
	let showItems = $derived(showsQuery.data?.items ?? []);
	let showTotal = $derived(showsQuery.data?.total ?? 0);

	let pendingFiles = $derived(
		(pendingQuery.data?.items ?? []).filter(
			(f) =>
				f.decision === "pending" &&
				(f.classification === "ambiguous" ||
					f.classification === "unmatched"),
		),
	);
	let pendingCount = $derived(pendingFiles.length);

	// commitableCount mirrors what the prototype shows on the "Commit N files"
	// button: confirmed/existing matches plus explicit accept decisions, minus
	// anything the reviewer marked skip. Counted over every row pendingQuery
	// loaded, which is the whole scan.
	let commitableCount = $derived(
		(pendingQuery.data?.items ?? []).filter(
			(f) =>
				f.decision !== "skip" &&
				(f.decision === "accept" ||
					f.classification === "confirmed" ||
					f.classification === "existing"),
		).length,
	);

	// Series equivalents of the two DecisionStrip counts, over shows.
	let pendingShowCount = $derived(pendingShows.length);
	let commitableShowCount = $derived(
		(pendingShowsQuery.data?.items ?? []).filter(
			(sh) =>
				sh.decision !== "skip" &&
				(sh.decision === "accept" ||
					sh.classification === "confirmed" ||
					sh.classification === "existing"),
		).length,
	);

	// Kind-aware values fed to the shared DecisionStrip.
	let stripPendingCount = $derived(isSeries ? pendingShowCount : pendingCount);
	let stripCommitableCount = $derived(
		isSeries ? commitableShowCount : commitableCount,
	);

	const cancel = createMutation<null, Error, void>(() => ({
		mutationFn: () =>
			api<null>(`/library/imports/${importId}/cancel`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["import", importId] });
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok(i18n.imports_scan_cancelled());
		},
		onError: (err) => toast.err(err.message),
	}));

	const commit = createMutation<ImportScan, Error, void>(() => ({
		mutationFn: () =>
			api<ImportScan>(`/library/imports/${importId}/commit`, {
				method: "POST",
			}),
		onSuccess: (resp) => {
			qc.setQueryData(["import", importId], resp);
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok(i18n.imports_commit_started());
		},
		onError: (err) => toast.err(err.message),
	}));

	const discard = createMutation<null, Error, void>(() => ({
		mutationFn: () =>
			api<null>(`/library/imports/${importId}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok(i18n.imports_scan_discarded());
			navigate("/library/imports");
		},
		onError: (err) => toast.err(err.message),
	}));

	// No bulk decision endpoint exists, so "Skip all unmatched" fans out
	// sequential PATCHes. Sequential keeps server load gentle and lets us
	// surface a partial-failure toast cleanly.
	const skipAll = createMutation<{ ok: number; fail: number }, Error, void>(
		() => ({
			mutationFn: async () => {
				let ok = 0;
				let fail = 0;
				for (const f of pendingFiles) {
					try {
						await api(
							`/library/imports/${importId}/files/${f.id}`,
							{ method: "PATCH", body: { decision: "skip" } },
						);
						ok++;
					} catch {
						fail++;
					}
				}
				return { ok, fail };
			},
			onSuccess: ({ ok, fail }) => {
				qc.invalidateQueries({
					queryKey: ["import", importId, "files"],
				});
				qc.invalidateQueries({
					queryKey: ["import", importId, "pending"],
				});
				if (fail === 0)
					toast.ok(
						ok === 1
							? i18n.imports_skipped_file_one({ count: ok })
							: i18n.imports_skipped_file_other({ count: ok }),
					);
				else toast.err(i18n.imports_skip_partial_files({ ok, fail }));
			},
			onError: (err) => toast.err(err.message),
		}),
	);

	// Series edition of the bulk skip: fans out sequential PATCHes to the
	// undecided (ambiguous/unmatched) show rows.
	const skipAllShows = createMutation<{ ok: number; fail: number }, Error, void>(
		() => ({
			mutationFn: async () => {
				let ok = 0;
				let fail = 0;
				for (const sh of pendingShows) {
					try {
						await api(
							`/library/imports/${importId}/shows/${sh.id}`,
							{ method: "PATCH", body: { decision: "skip" } },
						);
						ok++;
					} catch {
						fail++;
					}
				}
				return { ok, fail };
			},
			onSuccess: ({ ok, fail }) => {
				qc.invalidateQueries({ queryKey: ["import", importId, "shows"] });
				qc.invalidateQueries({
					queryKey: ["import", importId, "pending-shows"],
				});
				if (fail === 0)
					toast.ok(
						ok === 1
							? i18n.imports_skipped_show_one({ count: ok })
							: i18n.imports_skipped_show_other({ count: ok }),
					);
				else toast.err(i18n.imports_skip_partial_shows({ ok, fail }));
			},
			onError: (err) => toast.err(err.message),
		}),
	);

	// Match-picker: opening the AddMovieModal in "pick" mode for one file.
	// Seeded with the parsed title so the same TMDB candidates resurface (with
	// posters), while leaving the user free to search for a different match.
	let pickerFile = $state<ImportScanFile | null>(null);
	// Seed with the parsed title only — the search endpoint takes year as a
	// separate param the modal doesn't send, so folding it into the query text
	// would corrupt the TMDB search.
	let pickerSeed = $derived(pickerFile?.parsed_title ?? "");

	const pickMatch = createMutation<
		ImportScanFile,
		Error,
		{ fileId: number; tmdbId: number }
	>(() => ({
		mutationFn: ({ fileId, tmdbId }) =>
			api<ImportScanFile>(
				`/library/imports/${importId}/files/${fileId}`,
				{
					method: "PATCH",
					body: { decision: "accept", tmdb_id: tmdbId },
				},
			),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["import", importId, "files"] });
			qc.invalidateQueries({ queryKey: ["import", importId, "pending"] });
			toast.ok(i18n.imports_match_selected());
		},
		onError: (err) => toast.err(err.message),
	}));

	function onPickMatch(result: TMDBMovieResult) {
		if (!pickerFile) return;
		pickMatch.mutate({ fileId: pickerFile.id, tmdbId: result.tmdb_id });
		pickerFile = null;
	}

	// Series equivalent: AddSeriesModal in "pick" mode, seeded with the folder's
	// parsed title. Chosen show is committed via the same PATCH the row's skip
	// toggle uses, with decision=accept + the picked tvdb_id.
	let pickerShow = $state<ImportScanShow | null>(null);
	let pickerShowSeed = $derived(pickerShow?.parsed_title ?? "");

	const pickShowMatch = createMutation<
		ImportScanShow,
		Error,
		{ showId: number; tvdbId: number }
	>(() => ({
		mutationFn: ({ showId, tvdbId }) =>
			api<ImportScanShow>(
				`/library/imports/${importId}/shows/${showId}`,
				{
					method: "PATCH",
					body: { decision: "accept", tvdb_id: tvdbId },
				},
			),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["import", importId, "shows"] });
			qc.invalidateQueries({
				queryKey: ["import", importId, "pending-shows"],
			});
			toast.ok(i18n.imports_match_selected());
		},
		onError: (err) => toast.err(err.message),
	}));

	function onPickShowMatch(result: SeriesLookupResult) {
		if (!pickerShow) return;
		pickShowMatch.mutate({ showId: pickerShow.id, tvdbId: result.tvdb_id });
		pickerShow = null;
	}

	let items = $derived(filesQuery.data?.items ?? []);
	let total = $derived(filesQuery.data?.total ?? 0);

	// Client-side column sort layered over the (server-filtered, ≤200-row)
	// page. Search + classification stay server-side; sort just reorders what's
	// loaded.
	type FileSortKey = "file" | "classification" | "outcome";
	let sortKey = $state<FileSortKey | null>(null);
	let sortDir = $state<"asc" | "desc">("asc");

	const SORT_FIELD: Record<FileSortKey, (f: ImportScanFile) => string> = {
		file: (f) => f.source_path,
		classification: (f) => f.classification,
		// ponytail: sorts by the raw outcome field, not the row's effective
		// label (Will accept / Auto-accept …) which needs the row's render logic.
		outcome: (f) => f.outcome,
	};

	function toggleSort(key: FileSortKey) {
		if (sortKey === key) sortDir = sortDir === "asc" ? "desc" : "asc";
		else {
			sortKey = key;
			sortDir = "asc";
		}
	}

	let sortedItems = $derived.by(() => {
		if (!sortKey) return items;
		const get = SORT_FIELD[sortKey];
		const dir = sortDir === "asc" ? 1 : -1;
		return [...items].sort((a, b) => get(a).localeCompare(get(b)) * dir);
	});
	let isReviewing = $derived(scan?.status === "awaiting_review");
	let isLive = $derived(
		scan?.status === "running" || scan?.status === "committing",
	);
	let isTerminal = $derived(
		scan?.status === "completed" ||
			scan?.status === "cancelled" ||
			scan?.status === "failed",
	);
	let headerMeta = $derived(scan ? importStatusMeta(scan.status) : null);

	// ── Touch review (below lg) ─────────────────────────────────────────────
	// One row shape for the phone and the tablet, over one normalised entry, so
	// movie files and series folders render through the same list. Desktop keeps
	// its two tables and its two row components.
	let touchEntries = $derived<TouchEntry[]>(
		isSeries ? showItems.map(showEntry) : sortedItems.map(fileEntry),
	);
	let touchPending = $derived(
		isSeries ? showsQuery.isPending : filesQuery.isPending,
	);
	let touchError = $derived(
		isSeries ? showsQuery.error?.message : filesQuery.error?.message,
	);

	// The sheet tracks an id, not the entry: a refetch replaces the objects, and
	// holding one would leave the sheet showing a stale decision.
	let sheetId = $state<number | null>(null);
	let sheetEntry = $derived(
		sheetId == null
			? null
			: (touchEntries.find((e) => e.id === sheetId) ?? null),
	);

	function invalidateRows() {
		qc.invalidateQueries({
			queryKey: ["import", importId, isSeries ? "shows" : "files"],
		});
		qc.invalidateQueries({
			queryKey: ["import", importId, isSeries ? "pending-shows" : "pending"],
		});
	}

	// The desktop rows own their own skip mutation; the touch path needs one at
	// route level because the sheet is rendered here, not per row.
	const decideOne = createMutation<
		unknown,
		Error,
		{ id: number; decision: ImportFileDecision }
	>(() => ({
		mutationFn: ({ id, decision }) =>
			api(
				`/library/imports/${importId}/${isSeries ? "shows" : "files"}/${id}`,
				{ method: "PATCH", body: { decision } },
			),
		onSuccess: invalidateRows,
		onError: (err) => toast.err(err.message),
	}));

	function onSheetPick(entry: TouchEntry, candidateId: number) {
		if (isSeries)
			pickShowMatch.mutate({ showId: entry.id, tvdbId: candidateId });
		else pickMatch.mutate({ fileId: entry.id, tmdbId: candidateId });
	}

	// Hands off to the lookup surface the app already has, seeded with the parsed
	// title. The sheet closes first so the picker owns the screen.
	function onSheetSearch(entry: TouchEntry) {
		const rawShow = showItems.find((s) => s.id === entry.id);
		const rawFile = items.find((f) => f.id === entry.id);
		sheetId = null;
		if (isSeries) {
			if (rawShow) pickerShow = rawShow;
		} else if (rawFile) {
			pickerFile = rawFile;
		}
	}

	function onSheetSkip(entry: TouchEntry) {
		decideOne.mutate({
			id: entry.id,
			decision: entry.decision === "skip" ? "pending" : "skip",
		});
	}

	// Which picker surface. Below md the modal is replaced by ImportMatchSheet —
	// picking a match only PATCHes the scan row, so it has no business opening the
	// add-to-library modal.
	let isTouch = $state(false);
	onMount(() => {
		const mq = window.matchMedia("(max-width: 767px)");
		const sync = () => (isTouch = mq.matches);
		sync();
		mq.addEventListener("change", sync);
		return () => mq.removeEventListener("change", sync);
	});

	let pickerOpen = $derived(pickerFile !== null || pickerShow !== null);
	let pickerContext = $derived(
		pickerShow?.folder_path ?? pickerFile?.source_path ?? "",
	);

	function closePicker() {
		pickerFile = null;
		pickerShow = null;
	}

	function onSheetMatch(id: number) {
		if (pickerShow) pickShowMatch.mutate({ showId: pickerShow.id, tvdbId: id });
		else if (pickerFile) pickMatch.mutate({ fileId: pickerFile.id, tmdbId: id });
		closePicker();
	}
</script>

<div class="mx-auto w-full max-w-7xl px-4 py-6 md:px-8 md:py-7">
	<a
		href="/library/imports"
		class="inline-flex items-center gap-1.5 text-xs text-fg-subtle transition hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
	>
		<ArrowLeft size={14} aria-hidden="true" />
		{i18n.imports_label()}
	</a>

	{#if scanQuery.isPending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if scanQuery.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(scanQuery.error) })}
		</p>
	{:else if scan && headerMeta}
		<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
			<div class="min-w-0 flex-1">
				<p
					class="font-mono text-[10px] uppercase tracking-[0.18em] text-fg-faint"
				>
					{i18n.imports_scan_hash({ id: scan.id })}
				</p>
				<h1
					class="mt-1.5 break-all font-mono text-lg font-semibold text-fg"
					title={scan.source_path}
				>
					{scan.source_path}
				</h1>
				<div
					class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted"
				>
					<span
						class="rounded-sm border border-border bg-surface px-1.5 py-px text-[10px] font-medium uppercase tracking-wide text-fg-muted"
					>
						{importModeLabel(scan.mode, scan.import_mode)}
					</span>
					<span aria-hidden="true" class="text-fg-faint">·</span>
					<span title={formatDateTime(scan.created_at)}>
						{formatRelative(scan.created_at)}
					</span>
					{#if scan.total_count > 0}
						<span aria-hidden="true" class="text-fg-faint">·</span>
						<span class="font-mono tabular-nums">
							{i18n.imports_files_progress({
								done: scan.processed_count,
								total: scan.total_count,
							})}
						</span>
					{/if}
					{#if isTerminal}
						<span aria-hidden="true" class="text-fg-faint">·</span>
						<span class="font-mono tabular-nums">
							{i18n.imports_succeeded_failed({
								ok: scan.commit_success_count,
								fail: scan.commit_failed_count,
							})}
						</span>
					{/if}
				</div>
			</div>

			<div class="flex shrink-0 items-center gap-2">
				<span
					class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-1 text-[11px] font-semibold tracking-[0.02em]"
					style:background-color="var(--status-{headerMeta.kind})"
					style:color="var(--bg-deep)"
				>
					<span
						class="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--bg-deep)] {headerMeta.live
							? 'motion-safe:animate-pulse'
							: ''}"
						aria-hidden="true"
					></span>
					{headerMeta.label}
				</span>
				{#if isLive}
					<button
						type="button"
						disabled={cancel.isPending}
						onclick={() => (confirmCancel = true)}
						class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-card px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60"
					>
						<Square size={13} aria-hidden="true" />
						{cancel.isPending ? i18n.common_cancelling() : i18n.common_cancel()}
					</button>
				{:else if isReviewing}
					<button
						type="button"
						disabled={discard.isPending}
						onclick={() => (confirmDiscard = true)}
						class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-card px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:border-status-failed/50 hover:text-status-failed focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60"
					>
						<Trash2 size={13} aria-hidden="true" />
						{discard.isPending ? i18n.common_discarding() : i18n.common_discard()}
					</button>
				{/if}
			</div>
		</header>

		<div class="mt-5">
			<ImportSteps status={scan.status} series={isSeries} />
		</div>

		{#if scan.status === "failed"}
			<div
				class="mt-5 flex items-start gap-2 rounded-lg border border-status-failed/30 bg-status-failed/10 px-4 py-3 text-sm text-status-failed"
				role="alert"
			>
				<TriangleAlert
					size={16}
					class="mt-0.5 shrink-0"
					aria-hidden="true"
				/>
				<div class="min-w-0">
					<p class="font-semibold">{i18n.imports_scan_failed()}</p>
					{#if scan.failure_reason}
						<p class="mt-0.5 break-words text-xs">
							{scan.failure_reason}
						</p>
					{/if}
				</div>
			</div>
		{/if}

		{#if isReviewing}
			<div class="hidden lg:block">
				<DecisionStrip
					pendingCount={stripPendingCount}
					commitableCount={stripCommitableCount}
					noun={isSeries ? "show" : "file"}
					commitNote={commitNote(scan.mode, scan.import_mode)}
					skipBusy={isSeries ? skipAllShows.isPending : skipAll.isPending}
					commitBusy={commit.isPending}
					onSkipAll={() =>
						isSeries ? skipAllShows.mutate() : skipAll.mutate()}
					onCommit={() => commit.mutate()}
				/>
			</div>
		{/if}

		{#if isLive}
			<section
				class="mt-6 rounded-lg border border-border bg-bg-elevated p-6 md:p-8"
			>
				<ImportProgress {scan} />
			</section>
		{:else if isSeries}
			<section class="mt-6 hidden rounded-lg border border-border bg-bg-elevated lg:block">
				<header
					class="flex items-center justify-between border-b border-border px-5 py-3.5 md:px-6"
				>
					<h2 class="text-base font-semibold text-fg">{i18n.common_shows()}</h2>
					{#if showTotal > 0}
						<span class="font-mono text-xs tabular-nums text-fg-subtle">
							{showTotal}
						</span>
					{/if}
				</header>

				<div
					class="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 md:px-5"
				>
					<label class="relative min-w-0 flex-1">
						<span class="sr-only">{i18n.imports_search_shows()}</span>
						<input
							type="search"
							bind:value={q}
							placeholder={i18n.imports_search_folder_title()}
							class="w-full rounded-md border border-border bg-bg-card px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent-ring"
						/>
					</label>
					<div class="w-52 shrink-0">
						<Select
							value={classification}
							ariaLabel={i18n.imports_filter_by_classification()}
							onChange={(v) => (classification = v)}
							options={[
								{ value: "", label: i18n.imports_all_classifications() },
								{ value: "confirmed", label: i18n.imports_confirmed() },
								{ value: "ambiguous", label: i18n.imports_ambiguous() },
								{ value: "unmatched", label: i18n.imports_unmatched() },
								{ value: "existing", label: i18n.imports_existing() },
							]}
						/>
					</div>
				</div>

				<div class="overflow-x-auto">
					{#if showsQuery.isPending}
						<p class="px-5 py-8 text-sm text-fg-subtle">{i18n.common_loading_shows()}</p>
					{:else if showsQuery.isError}
						<p class="px-5 py-8 text-sm text-status-failed">
							Failed: {showsQuery.error?.message}
						</p>
					{:else if showItems.length === 0}
						<p class="px-5 py-8 text-sm text-fg-muted">
							{i18n.imports_no_shows_match()}
						</p>
					{:else}
						<table class="w-full text-sm">
							<thead
								class="bg-surface text-left text-[10px] uppercase tracking-[0.14em] text-fg-faint"
							>
								<tr>
									<th class="px-4 py-2.5 font-semibold">{i18n.imports_show_folder()}</th>
									<th class="hidden px-4 py-2.5 font-semibold md:table-cell">
										{i18n.imports_classification()}
									</th>
									<th class="hidden px-4 py-2.5 font-semibold md:table-cell">
										{i18n.common_outcome()}
									</th>
									<th class="px-4 py-2.5 text-right font-semibold">
										{i18n.common_decision()}
									</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#each showItems as sh (sh.id)}
									<ImportShowRow
										show={sh}
										scanId={importId}
										reviewing={isReviewing}
										onChooseMatch={(show) =>
											(pickerShow = show)}
									/>
								{/each}
							</tbody>
						</table>
					{/if}
				</div>
			</section>
		{:else}
			<section class="mt-6 hidden rounded-lg border border-border bg-bg-elevated lg:block">
				<header
					class="flex items-center justify-between border-b border-border px-5 py-3.5 md:px-6"
				>
					<h2 class="text-base font-semibold text-fg">{i18n.common_files()}</h2>
					{#if total > 0}
						<span
							class="font-mono text-xs tabular-nums text-fg-subtle"
						>
							{total}
						</span>
					{/if}
				</header>

				<div
					class="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 md:px-5"
				>
					<label class="relative min-w-0 flex-1">
						<span class="sr-only">{i18n.imports_search_filenames()}</span>
						<input
							type="search"
							bind:value={q}
							placeholder={i18n.imports_search_filename()}
							class="w-full rounded-md border border-border bg-bg-card px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent-ring"
						/>
					</label>
					<div class="w-52 shrink-0">
						<Select
							value={classification}
							ariaLabel={i18n.imports_filter_by_classification()}
							onChange={(v) => (classification = v)}
							options={[
								{ value: "", label: i18n.imports_all_classifications() },
								{ value: "confirmed", label: i18n.imports_confirmed() },
								{ value: "ambiguous", label: i18n.imports_ambiguous() },
								{ value: "unmatched", label: i18n.imports_unmatched() },
								{ value: "existing", label: i18n.imports_existing() },
							]}
						/>
					</div>
				</div>

				<div class="overflow-x-auto">
					{#if filesQuery.isPending}
						<p class="px-5 py-8 text-sm text-fg-subtle">
							{i18n.common_loading_files()}
						</p>
					{:else if filesQuery.isError}
						<p class="px-5 py-8 text-sm text-status-failed">
							Failed: {filesQuery.error?.message}
						</p>
					{:else if items.length === 0}
						<p class="px-5 py-8 text-sm text-fg-muted">
							{i18n.imports_no_files_match()}
						</p>
					{:else}
						<table class="w-full text-sm">
							<thead
								class="bg-surface text-left text-[10px] uppercase tracking-[0.14em] text-fg-faint"
							>
								<tr>
									{@render sortHeader("file", i18n.common_file())}
									{@render sortHeader(
										"classification",
										i18n.imports_classification(),
										false,
									)}
									{@render sortHeader("outcome", i18n.common_outcome(), false)}
									<th class="px-4 py-2.5 text-right font-semibold">
										{i18n.common_decision()}
									</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#each sortedItems as f (f.id)}
									<ImportFileRow
										file={f}
										scanId={importId}
										reviewing={isReviewing}
										onChooseMatch={(file) =>
											(pickerFile = file)}
									/>
								{/each}
							</tbody>
						</table>
					{/if}
				</div>
			</section>
		{/if}

		{#if !isLive}
			<ImportTouchList
				entries={touchEntries}
				total={isSeries ? showTotal : total}
				series={isSeries}
				query={q}
				onQueryChange={(v) => (q = v)}
				{classification}
				onClassificationChange={(c) => (classification = c)}
				pending={touchPending}
				error={touchError}
				onOpen={(e) => (sheetId = e.id)}
			/>
			{#if isReviewing}
				<ImportCommitBar
					pendingCount={stripPendingCount}
					commitableCount={stripCommitableCount}
					series={isSeries}
					commitSummary={commitSummary(scan.mode, scan.import_mode)}
					skipBusy={isSeries ? skipAllShows.isPending : skipAll.isPending}
					commitBusy={commit.isPending}
					onSkipAll={() =>
						isSeries ? skipAllShows.mutate() : skipAll.mutate()}
					onCommit={() => commit.mutate()}
				/>
			{/if}
		{/if}
	{/if}
</div>

<ImportDecisionSheet
	entry={sheetEntry}
	series={isSeries}
	reviewing={isReviewing}
	busy={decideOne.isPending || pickMatch.isPending || pickShowMatch.isPending}
	onClose={() => (sheetId = null)}
	onPick={onSheetPick}
	onSearch={onSheetSearch}
	onSkipToggle={onSheetSkip}
/>

{#snippet sortHeader(key: FileSortKey, label: string, phone = true)}
	{@const active = sortKey === key}
	<th
		class={cn("px-4 py-2.5", !phone && "hidden md:table-cell")}
		aria-sort={active
			? sortDir === "asc"
				? "ascending"
				: "descending"
			: "none"}
	>
		<button
			type="button"
			onclick={() => toggleSort(key)}
			class={cn(
				"inline-flex items-center gap-1 font-semibold uppercase tracking-[0.14em] transition-colors",
				active ? "text-fg-muted" : "hover:text-fg-muted",
			)}
		>
			{label}
			{#if active}
				{#if sortDir === "asc"}
					<ArrowUp size={11} class="text-accent" aria-hidden="true" />
				{:else}
					<ArrowDown size={11} class="text-accent" aria-hidden="true" />
				{/if}
			{:else}
				<ArrowUpDown size={11} class="text-fg-faint" aria-hidden="true" />
			{/if}
		</button>
	</th>
{/snippet}

{#if isTouch}
	<ImportMatchSheet
		open={pickerOpen}
		series={isSeries}
		seed={isSeries ? pickerShowSeed : pickerSeed}
		context={pickerContext}
		busy={pickMatch.isPending || pickShowMatch.isPending}
		onClose={closePicker}
		onPick={onSheetMatch}
	/>
{:else}
	<AddMovieModal
		open={pickerFile !== null}
		mode="pick"
		seedQuery={pickerSeed}
		onPick={onPickMatch}
		onClose={() => (pickerFile = null)}
	/>

	<AddSeriesModal
		open={pickerShow !== null}
		mode="pick"
		seedQuery={pickerShowSeed}
		onPick={onPickShowMatch}
		onClose={() => (pickerShow = null)}
	/>
{/if}

<Dialog
	open={confirmCancel}
	title={i18n.imports_cancel_confirm()}
	body={i18n.imports_cancel_confirm_body()}
	onClose={() => (confirmCancel = false)}
	actions={[
		{ label: i18n.imports_keep_scanning(), variant: "ghost", autofocus: true },
		{ label: i18n.imports_cancel_scan(), variant: "danger", onClick: () => cancel.mutate() },
	]}
/>

<Dialog
	open={confirmDiscard}
	title={i18n.imports_discard_confirm()}
	body={i18n.imports_discard_confirm_body()}
	onClose={() => (confirmDiscard = false)}
	actions={[
		{ label: i18n.common_keep(), variant: "ghost", autofocus: true },
		{ label: i18n.common_discard(), variant: "danger", onClick: () => discard.mutate() },
	]}
/>

