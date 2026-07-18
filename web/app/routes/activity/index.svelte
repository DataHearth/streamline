<script lang="ts">
	import { slide } from "svelte/transition";
	import { ChevronDown } from "@lucide/svelte";
	import {
		createQuery,
		createInfiniteQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import type {
		DownloadQueue,
		DownloadHistory,
		QueueEntry,
		HistoryEntry,
		PendingList,
		PendingItem,
		Torrent,
		TorrentList,
		TorrentDetails,
		TorrentAddResult,
		TorrentFilePriority,
		AddTorrentRequest,
	} from "../../lib/types";
	import ActivityToolbar from "../../components/activity/ActivityToolbar.svelte";
	import ActivityTable from "../../components/activity/ActivityTable.svelte";
	import LiveStrip from "../../components/activity/LiveStrip.svelte";
	import PendingRow from "../../components/pending/PendingRow.svelte";
	import TorrentTable from "../../components/activity/TorrentTable.svelte";
	import TorrentDrawer from "../../components/activity/TorrentDrawer.svelte";
	import AddTorrentModal from "../../components/activity/AddTorrentModal.svelte";
	import { formatSpeed } from "../../lib/format";

	type View = "queue" | "history" | "torrents";

	let view = $state<View>("queue");
	let statusFilter = $state<string[]>([]);
	let search = $state("");
	let selectedHash = $state<string | null>(null);
	let addOpen = $state(false);
	let density = $state<"comfortable" | "compact">(
		(typeof localStorage !== "undefined" &&
			(localStorage.getItem("streamline:activity-density") as
				| "comfortable"
				| "compact")) ||
			"comfortable",
	);
	$effect(() => {
		if (typeof localStorage !== "undefined")
			localStorage.setItem("streamline:activity-density", density);
	});

	const qc = useQueryClient();

	const queue = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		refetchInterval: 2000,
	}));

	const PAGE = 50;
	const history = createInfiniteQuery<
		DownloadHistory,
		Error,
		{ pages: DownloadHistory[]; pageParams: (string | null)[] },
		readonly ["activity", "history"],
		string | null
	>(() => ({
		queryKey: ["activity", "history"] as const,
		queryFn: ({ pageParam }) => {
			const p = new URLSearchParams({ limit: String(PAGE) });
			if (pageParam) p.set("cursor", pageParam);
			return api<DownloadHistory>(`/activity/history?${p.toString()}`);
		},
		initialPageParam: null,
		getNextPageParam: (last) => last.next_cursor ?? undefined,
	}));

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["activity", "queue"] });
		qc.invalidateQueries({ queryKey: ["activity", "history"] });
	}

	const cancel = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) => api(`/activity/queue/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			toast.ok("Download cancelled");
			invalidate();
		},
		onError: (e) => toast.err(e.message),
	}));
	const pause = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) =>
			api(`/activity/queue/${id}/pause`, { method: "POST" }),
		onSuccess: invalidate,
		onError: (e) => toast.err(e.message),
	}));
	const resume = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) =>
			api(`/activity/queue/${id}/resume`, { method: "POST" }),
		onSuccess: invalidate,
		onError: (e) => toast.err(e.message),
	}));
	const removeHistory = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) =>
			api(`/activity/history/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			toast.ok("Removed");
			invalidate();
		},
		onError: (e) => toast.err(e.message),
	}));
	const clearCompleted = createMutation<unknown, Error, void>(() => ({
		mutationFn: () =>
			api("/activity/history/clear-completed", { method: "POST" }),
		onSuccess: () => {
			toast.ok("Cleared completed");
			invalidate();
		},
		onError: (e) => toast.err(e.message),
	}));

	// "Needs attention": adopted-torrent proposals awaiting a decision (admin).
	const pendingQuery = createQuery<PendingList>(() => ({
		queryKey: ["activity", "pending"],
		queryFn: () => api<PendingList>("/activity/pending"),
		enabled: auth.isAdmin,
		refetchInterval: 30000,
	}));
	let pendingItems = $derived<PendingItem[]>(pendingQuery.data?.items ?? []);
	// Collapsed by default on phones so it doesn't bury the queue/history table.
	let attnOpen = $state(
		typeof window === "undefined" ||
			!window.matchMedia("(max-width: 767px)").matches,
	);

	function invalidatePending() {
		qc.invalidateQueries({ queryKey: ["activity", "pending"] });
	}

	const importPending = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) =>
			api(`/activity/pending/${id}/import`, { method: "POST" }),
		onSuccess: () => {
			toast.ok("Importing");
			invalidatePending();
			invalidate();
		},
		onError: (e) => toast.err(e.message),
	}));
	const replacePending = createMutation<
		unknown,
		Error,
		{ id: number; removeOld: boolean }
	>(() => ({
		mutationFn: ({ id, removeOld }) =>
			api(`/activity/pending/${id}/replace`, {
				method: "POST",
				body: { remove_old_torrent: removeOld },
			}),
		onSuccess: () => {
			toast.ok("Replacing");
			invalidatePending();
			invalidate();
		},
		onError: (e) => toast.err(e.message),
	}));
	const ignorePending = createMutation<
		unknown,
		Error,
		{ id: number; removeTorrent: boolean }
	>(() => ({
		mutationFn: ({ id, removeTorrent }) =>
			api(`/activity/pending/${id}/ignore`, {
				method: "POST",
				body: { remove_torrent: removeTorrent },
			}),
		onSuccess: () => {
			toast.ok("Ignored");
			invalidatePending();
		},
		onError: (e) => toast.err(e.message),
	}));

	let pendingBusyId = $derived.by<number | null>(() => {
		if (importPending.isPending) return importPending.variables ?? null;
		if (replacePending.isPending) return replacePending.variables?.id ?? null;
		if (ignorePending.isPending) return ignorePending.variables?.id ?? null;
		return null;
	});

	// ── Built-in torrents ────────────────────────────────────────────────
	const torrents = createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		enabled: view === "torrents",
		refetchInterval: 2000,
	}));
	// The list stays light — files / peers / trackers come from a per-torrent
	// detail query that polls only while the drawer is open (2 s), per the
	// reconciled contract.
	const torrentDetail = createQuery<TorrentDetails>(() => ({
		queryKey: ["torrents", "detail", selectedHash],
		queryFn: () => api<TorrentDetails>(`/torrents/${selectedHash}`),
		enabled: view === "torrents" && !!selectedHash,
		refetchInterval: 2000,
	}));
	function invalidateTorrents() {
		qc.invalidateQueries({ queryKey: ["activity", "torrents"] });
	}
	function invalidateTorrentDetail() {
		qc.invalidateQueries({ queryKey: ["torrents", "detail"] });
	}

	const addTorrent = createMutation<TorrentAddResult, Error, AddTorrentRequest>(
		() => ({
			mutationFn: (body) =>
				api<TorrentAddResult>("/torrents", { method: "POST", body }),
			onSuccess: (_res, vars) => {
				invalidateTorrents();
				addOpen = false;
				toast.ok(
					vars.magnet
						? "Magnet added — fetching metadata"
						: "Torrent added",
				);
			},
			onError: (e) => toast.err(e.message),
		}),
	);
	const pauseTorrent = createMutation<unknown, Error, string>(() => ({
		mutationFn: (hash) => api(`/torrents/${hash}/pause`, { method: "POST" }),
		onSuccess: invalidateTorrents,
		onError: (e) => toast.err(e.message),
	}));
	const resumeTorrent = createMutation<unknown, Error, string>(() => ({
		mutationFn: (hash) => api(`/torrents/${hash}/resume`, { method: "POST" }),
		onSuccess: invalidateTorrents,
		onError: (e) => toast.err(e.message),
	}));
	const removeTorrent = createMutation<
		unknown,
		Error,
		{ hash: string; deleteFiles: boolean }
	>(() => ({
		// delete_files is a query param — DELETE request bodies are poorly
		// supported (reconciled contract).
		mutationFn: ({ hash, deleteFiles }) =>
			api(`/torrents/${hash}?delete_files=${deleteFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			invalidateTorrents();
			selectedHash = null;
			toast.ok("Torrent removed");
		},
		onError: (e) => toast.err(e.message),
	}));
	const setPriority = createMutation<
		unknown,
		Error,
		{ hash: string; index: number; priority: TorrentFilePriority }
	>(() => ({
		mutationFn: ({ hash, index, priority }) =>
			api(`/torrents/${hash}/files/${index}`, {
				method: "PATCH",
				body: { priority },
			}),
		onSuccess: () => {
			invalidateTorrents();
			invalidateTorrentDetail();
		},
		onError: (e) => toast.err(e.message),
	}));

	let torrentItems = $derived<Torrent[]>(torrents.data?.items ?? []);
	let torrentsNotConfigured = $derived(
		view === "torrents" &&
			torrents.isError &&
			(torrents.error as { status?: number } | null)?.status === 404,
	);
	let torrentRows = $derived.by<Torrent[]>(() => {
		let out = torrentItems;
		if (statusFilter.length)
			out = out.filter((t) => statusFilter.includes(t.status));
		if (search.trim()) {
			const q = search.toLowerCase();
			out = out.filter(
				(t) =>
					t.name.toLowerCase().includes(q) ||
					t.hash.toLowerCase().includes(q),
			);
		}
		return out;
	});
	let selectedTorrent = $derived(
		torrentItems.find((t) => t.hash === selectedHash) ?? null,
	);
	let selectedDetail = $derived<TorrentDetails | null>(
		selectedHash && torrentDetail.data?.hash === selectedHash
			? torrentDetail.data
			: null,
	);
	let torrentBusyHash = $derived.by<string | null>(() => {
		if (pauseTorrent.isPending) return pauseTorrent.variables ?? null;
		if (resumeTorrent.isPending) return resumeTorrent.variables ?? null;
		if (removeTorrent.isPending) return removeTorrent.variables?.hash ?? null;
		return null;
	});
	let busyFileIndex = $derived.by<number | null>(() =>
		setPriority.isPending ? (setPriority.variables?.index ?? null) : null,
	);
	// Live aggregate stats for the torrents summary strip.
	let torrentDownloading = $derived(
		torrentItems.filter((t) => t.status === "downloading").length,
	);
	let torrentSeeding = $derived(
		torrentItems.filter((t) => t.status === "seeding").length,
	);
	let torrentAggDown = $derived(
		torrentItems.reduce((s, t) => s + (t.download_speed ?? 0), 0),
	);
	let torrentAggUp = $derived(
		torrentItems.reduce((s, t) => s + (t.upload_speed ?? 0), 0),
	);

	let queueItems = $derived<QueueEntry[]>(queue.data?.items ?? []);
	let historyItems = $derived<HistoryEntry[]>(
		(history.data?.pages ?? []).flatMap((p) => p.items),
	);

	let rows = $derived.by<(QueueEntry | HistoryEntry)[]>(() => {
		const src: (QueueEntry | HistoryEntry)[] =
			view === "queue" ? queueItems : historyItems;
		let out = src;
		if (statusFilter.length)
			out = out.filter((i) => statusFilter.includes(i.status));
		if (search.trim()) {
			const q = search.toLowerCase();
			out = out.filter(
				(i) =>
					i.title.toLowerCase().includes(q) ||
					i.movie.title.toLowerCase().includes(q) ||
					(i.episode?.show_title ?? "").toLowerCase().includes(q),
			);
		}
		return out;
	});

	let busyId = $derived.by<number | null>(() => {
		if (cancel.isPending) return cancel.variables ?? null;
		if (pause.isPending) return pause.variables ?? null;
		if (resume.isPending) return resume.variables ?? null;
		if (removeHistory.isPending) return removeHistory.variables ?? null;
		return null;
	});
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<header class="mb-4">
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			Queue &amp; History
		</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{#if view === "torrents"}
				{torrentItems.length} torrent{torrentItems.length === 1 ? "" : "s"} · built-in engine
			{:else}
				{queueItems.length} active · {historyItems.length} in history
			{/if}
		</p>
	</header>

	{#if auth.isAdmin && (pendingItems.length > 0 || pendingQuery.isError)}
		<section
			class="mb-4 rounded-xl border border-status-wanted/30 bg-status-wanted/[0.04] p-4"
		>
			<button
				type="button"
				onclick={() => (attnOpen = !attnOpen)}
				aria-expanded={attnOpen}
				class="flex w-full items-center gap-2 text-left {attnOpen ? 'mb-3' : ''}"
			>
				<h2 class="text-sm font-semibold text-fg">Needs attention</h2>
				{#if pendingItems.length > 0}
					<span
						class="rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
					>
						{pendingItems.length}
					</span>
				{/if}
				<ChevronDown
					size={16}
					class="ml-auto shrink-0 text-fg-muted transition-transform {attnOpen
						? 'rotate-180'
						: ''}"
					aria-hidden="true"
				/>
			</button>
			{#if attnOpen}
				<div transition:slide={{ duration: 180 }}>
					{#if pendingQuery.isError}
						<p class="text-sm text-status-failed">
							Failed to load proposals.
						</p>
					{:else}
						<div class="flex flex-col gap-2">
							{#each pendingItems as item (item.id)}
								<PendingRow
									{item}
									busy={pendingBusyId === item.id}
									onImport={() => importPending.mutate(item.id)}
									onReplace={(removeOld) =>
										replacePending.mutate({ id: item.id, removeOld })}
									onIgnore={(removeTorrent) =>
										ignorePending.mutate({ id: item.id, removeTorrent })}
								/>
							{/each}
						</div>
					{/if}
				</div>
			{/if}
		</section>
	{/if}

	{#if view === "torrents"}
		{#if !torrentsNotConfigured}
			<div
				class="mb-4 grid grid-cols-2 gap-4 rounded-lg border border-border bg-bg-elevated px-5 py-4 sm:grid-cols-4"
			>
				<div>
					<div class="text-2xl font-bold tabular-nums text-fg">
						{torrentDownloading}
					</div>
					<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
						Downloading
					</div>
				</div>
				<div>
					<div class="text-2xl font-bold tabular-nums text-status-seeding">
						{torrentSeeding}
					</div>
					<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
						Seeding
					</div>
				</div>
				<div>
					<div class="text-2xl font-bold tabular-nums text-status-downloading">
						{formatSpeed(torrentAggDown) || "—"}
					</div>
					<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
						Aggregate ↓
					</div>
				</div>
				<div>
					<div class="text-2xl font-bold tabular-nums text-status-seeding">
						{formatSpeed(torrentAggUp) || "—"}
					</div>
					<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
						Aggregate ↑
					</div>
				</div>
			</div>
		{/if}
	{:else}
		<LiveStrip items={queueItems} />
	{/if}

	<ActivityToolbar
		{view}
		{statusFilter}
		{search}
		{density}
		clearableCount={historyItems.filter((h) => h.status === "completed").length}
		onViewChange={(v) => {
			view = v;
			statusFilter = [];
			if (v !== "torrents") selectedHash = null;
		}}
		onStatusFilterChange={(s) => (statusFilter = s)}
		onSearchChange={(q) => (search = q)}
		onDensityToggle={() =>
			(density = density === "comfortable" ? "compact" : "comfortable")}
		onClearCompleted={auth.isAdmin
			? () => clearCompleted.mutate()
			: undefined}
		onAddTorrent={() => (addOpen = true)}
		canAddTorrent={auth.isAdmin && !torrentsNotConfigured}
	/>

	{#if view === "torrents"}
		<TorrentTable
			rows={torrentRows}
			{density}
			loading={torrents.isPending && !torrentsNotConfigured}
			error={torrentsNotConfigured ? null : (torrents.error ?? null)}
			notConfigured={torrentsNotConfigured}
			canControl={auth.isAdmin}
			{selectedHash}
			onOpen={(h) => (selectedHash = h)}
			onAdd={() => (addOpen = true)}
		/>
	{:else}
		<ActivityTable
			{view}
			{density}
			{rows}
			{busyId}
			loading={view === "queue" ? queue.isPending : history.isPending}
			error={view === "queue"
				? (queue.error ?? null)
				: (history.error ?? null)}
			hasMore={view === "history" && (history.hasNextPage ?? false)}
			loadingMore={history.isFetchingNextPage}
			canControl={auth.isAdmin}
			onLoadMore={() => history.fetchNextPage()}
			onCancel={(id) => cancel.mutate(id)}
			onPause={(id) => pause.mutate(id)}
			onResume={(id) => resume.mutate(id)}
			onRemove={(id) => removeHistory.mutate(id)}
		/>
	{/if}
</div>

<TorrentDrawer
	open={view === "torrents" && !!selectedTorrent}
	torrent={selectedTorrent}
	detail={selectedDetail}
	canControl={auth.isAdmin}
	busy={torrentBusyHash !== null && torrentBusyHash === selectedTorrent?.hash}
	{busyFileIndex}
	onClose={() => (selectedHash = null)}
	onPause={(hash) => pauseTorrent.mutate(hash)}
	onResume={(hash) => resumeTorrent.mutate(hash)}
	onRemove={(hash, deleteFiles) => removeTorrent.mutate({ hash, deleteFiles })}
	onSetPriority={(hash, index, priority) =>
		setPriority.mutate({ hash, index, priority })}
/>

<AddTorrentModal
	open={addOpen}
	busy={addTorrent.isPending}
	onClose={() => (addOpen = false)}
	onAdd={(payload) => addTorrent.mutate(payload)}
/>
