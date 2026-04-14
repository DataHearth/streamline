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
	import { api } from "../../../lib/api";
	import { cn } from "../../../lib/cn";
	import { formatDateTime, formatRelative } from "../../../lib/dates";
	import {
		commitVerb,
		importModeLabel,
		importStatusMeta,
	} from "../../../lib/imports";
	import { toast } from "../../../lib/toast";
	import type {
		ImportFileClassification,
		ImportScan,
		ImportScanFile,
		ImportScanFileList,
		ImportScanShow,
		ImportScanShowList,
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
				`Commit finished — ${scan.commit_success_count} imported, ${scan.commit_failed_count} failed`,
			);
		}
		prevStatus = cur;
	});

	let q = $state("");
	let classification = $state<ImportFileClassification | "">("");
	const FILE_LIMIT = 200;

	const filesQuery = createQuery<ImportScanFileList>(() => ({
		queryKey: ["import", importId, "files", { q, classification }],
		queryFn: () => {
			const sp = new URLSearchParams({
				page: "1",
				limit: String(FILE_LIMIT),
			});
			if (q.trim()) sp.set("q", q.trim());
			if (classification) sp.set("classification", classification);
			return api<ImportScanFileList>(
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
	const pendingQuery = createQuery<ImportScanFileList>(() => ({
		queryKey: ["import", importId, "pending"],
		queryFn: () => {
			const sp = new URLSearchParams({
				page: "1",
				limit: String(FILE_LIMIT),
			});
			return api<ImportScanFileList>(
				`/library/imports/${importId}/files?${sp}`,
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
	const showsQuery = createQuery<ImportScanShowList>(() => ({
		queryKey: ["import", importId, "shows", { classification }],
		queryFn: () => {
			const sp = new URLSearchParams({
				page: "1",
				limit: String(FILE_LIMIT),
			});
			if (classification) sp.set("classification", classification);
			return api<ImportScanShowList>(
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

	const pendingShowsQuery = createQuery<ImportScanShowList>(() => ({
		queryKey: ["import", importId, "pending-shows"],
		queryFn: () => {
			const sp = new URLSearchParams({
				page: "1",
				limit: String(FILE_LIMIT),
			});
			return api<ImportScanShowList>(
				`/library/imports/${importId}/shows?${sp}`,
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
	// anything the reviewer marked skip. Capped at FILE_LIMIT because that's
	// what pendingQuery loads.
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
			toast.ok("Scan cancelled");
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
			toast.ok("Commit started");
		},
		onError: (err) => toast.err(err.message),
	}));

	const discard = createMutation<null, Error, void>(() => ({
		mutationFn: () =>
			api<null>(`/library/imports/${importId}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok("Scan discarded");
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
				if (fail === 0) toast.ok(`Skipped ${ok} file${ok === 1 ? "" : "s"}`);
				else
					toast.err(
						`Skipped ${ok}, failed on ${fail} — review the remaining files manually`,
					);
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
				if (fail === 0) toast.ok(`Skipped ${ok} show${ok === 1 ? "" : "s"}`);
				else
					toast.err(
						`Skipped ${ok}, failed on ${fail} — review the remaining shows manually`,
					);
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
			toast.ok("Match selected");
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
			toast.ok("Match selected");
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
</script>

<div class="mx-auto w-full max-w-7xl px-4 py-6 md:px-8 md:py-7">
	<a
		href="/library/imports"
		class="inline-flex items-center gap-1.5 text-xs text-fg-subtle transition hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
	>
		<ArrowLeft size={14} aria-hidden="true" />
		Imports
	</a>

	{#if scanQuery.isPending}
		<p class="mt-6 text-sm text-fg-subtle">Loading…</p>
	{:else if scanQuery.isError}
		<p class="mt-6 text-sm text-status-failed">
			Failed to load: {scanQuery.error?.message}
		</p>
	{:else if scan && headerMeta}
		<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
			<div class="min-w-0 flex-1">
				<p
					class="font-mono text-[10px] uppercase tracking-[0.18em] text-fg-faint"
				>
					Import scan · #{scan.id}
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
							{scan.processed_count}/{scan.total_count} files
						</span>
					{/if}
					{#if isTerminal}
						<span aria-hidden="true" class="text-fg-faint">·</span>
						<span class="font-mono tabular-nums">
							{scan.commit_success_count} succeeded · {scan.commit_failed_count} failed
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
						{cancel.isPending ? "Cancelling…" : "Cancel"}
					</button>
				{:else if isReviewing}
					<button
						type="button"
						disabled={discard.isPending}
						onclick={() => (confirmDiscard = true)}
						class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-card px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:border-status-failed/50 hover:text-status-failed focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60"
					>
						<Trash2 size={13} aria-hidden="true" />
						{discard.isPending ? "Discarding…" : "Discard"}
					</button>
				{/if}
			</div>
		</header>

		<div class="mt-5">
			<ImportSteps status={scan.status} />
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
					<p class="font-semibold">Scan failed</p>
					{#if scan.failure_reason}
						<p class="mt-0.5 break-words text-xs">
							{scan.failure_reason}
						</p>
					{/if}
				</div>
			</div>
		{/if}

		{#if isReviewing}
			<DecisionStrip
				pendingCount={stripPendingCount}
				commitableCount={stripCommitableCount}
				noun={isSeries ? "show" : "file"}
				commitVerb={commitVerb(scan.mode, scan.import_mode)}
				skipBusy={isSeries ? skipAllShows.isPending : skipAll.isPending}
				commitBusy={commit.isPending}
				onSkipAll={() =>
					isSeries ? skipAllShows.mutate() : skipAll.mutate()}
				onCommit={() => commit.mutate()}
			/>
		{/if}

		{#if isLive}
			<section
				class="mt-6 rounded-lg border border-border bg-bg-elevated p-6 md:p-8"
			>
				<ImportProgress {scan} />
			</section>
		{:else if isSeries}
			<section class="mt-6 rounded-lg border border-border bg-bg-elevated">
				<header
					class="flex items-center justify-between border-b border-border px-5 py-3.5 md:px-6"
				>
					<h2 class="text-base font-semibold text-fg">Shows</h2>
					{#if showTotal > 0}
						<span class="font-mono text-xs tabular-nums text-fg-subtle">
							{showTotal}
						</span>
					{/if}
				</header>

				<div
					class="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 md:px-5"
				>
					<div class="w-52 shrink-0">
						<Select
							value={classification}
							ariaLabel="Filter by classification"
							onChange={(v) => (classification = v)}
							options={[
								{ value: "", label: "All classifications" },
								{ value: "confirmed", label: "Confirmed" },
								{ value: "ambiguous", label: "Ambiguous" },
								{ value: "unmatched", label: "Unmatched" },
								{ value: "existing", label: "Existing" },
							]}
						/>
					</div>
				</div>

				<div class="overflow-x-auto">
					{#if showsQuery.isPending}
						<p class="px-5 py-8 text-sm text-fg-subtle">Loading shows…</p>
					{:else if showsQuery.isError}
						<p class="px-5 py-8 text-sm text-status-failed">
							Failed: {showsQuery.error?.message}
						</p>
					{:else if showItems.length === 0}
						<p class="px-5 py-8 text-sm text-fg-muted">
							No shows match this filter.
						</p>
					{:else}
						<table class="w-full text-sm">
							<thead
								class="bg-surface text-left text-[10px] uppercase tracking-[0.14em] text-fg-faint"
							>
								<tr>
									<th class="px-4 py-2.5 font-semibold">Show folder</th>
									<th class="px-4 py-2.5 font-semibold">
										Classification
									</th>
									<th class="px-4 py-2.5 font-semibold">Outcome</th>
									<th class="px-4 py-2.5 text-right font-semibold">
										Decision
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
			<section class="mt-6 rounded-lg border border-border bg-bg-elevated">
				<header
					class="flex items-center justify-between border-b border-border px-5 py-3.5 md:px-6"
				>
					<h2 class="text-base font-semibold text-fg">Files</h2>
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
						<span class="sr-only">Search filenames</span>
						<input
							type="search"
							bind:value={q}
							placeholder="Search filename…"
							class="w-full rounded-md border border-border bg-bg-card px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent-ring"
						/>
					</label>
					<div class="w-52 shrink-0">
						<Select
							value={classification}
							ariaLabel="Filter by classification"
							onChange={(v) => (classification = v)}
							options={[
								{ value: "", label: "All classifications" },
								{ value: "confirmed", label: "Confirmed" },
								{ value: "ambiguous", label: "Ambiguous" },
								{ value: "unmatched", label: "Unmatched" },
								{ value: "existing", label: "Existing" },
							]}
						/>
					</div>
				</div>

				<div class="overflow-x-auto">
					{#if filesQuery.isPending}
						<p class="px-5 py-8 text-sm text-fg-subtle">
							Loading files…
						</p>
					{:else if filesQuery.isError}
						<p class="px-5 py-8 text-sm text-status-failed">
							Failed: {filesQuery.error?.message}
						</p>
					{:else if items.length === 0}
						<p class="px-5 py-8 text-sm text-fg-muted">
							No files match this filter.
						</p>
					{:else}
						<table class="w-full text-sm">
							<thead
								class="bg-surface text-left text-[10px] uppercase tracking-[0.14em] text-fg-faint"
							>
								<tr>
									{@render sortHeader("file", "File")}
									{@render sortHeader(
										"classification",
										"Classification",
									)}
									{@render sortHeader("outcome", "Outcome")}
									<th class="px-4 py-2.5 text-right font-semibold">
										Decision
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
	{/if}
</div>

{#snippet sortHeader(key: FileSortKey, label: string)}
	{@const active = sortKey === key}
	<th
		class="px-4 py-2.5"
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

<Dialog
	open={confirmCancel}
	title="Cancel this scan?"
	body="The running import scan will be stopped."
	onClose={() => (confirmCancel = false)}
	actions={[
		{ label: "Keep scanning", variant: "ghost", autofocus: true },
		{ label: "Cancel scan", variant: "danger", onClick: () => cancel.mutate() },
	]}
/>

<Dialog
	open={confirmDiscard}
	title="Discard this scan?"
	body="All decisions made for this scan will be lost."
	onClose={() => (confirmDiscard = false)}
	actions={[
		{ label: "Keep", variant: "ghost", autofocus: true },
		{ label: "Discard", variant: "danger", onClick: () => discard.mutate() },
	]}
/>

