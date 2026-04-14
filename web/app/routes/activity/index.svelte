<script lang="ts">
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
	} from "../../lib/types";
	import ActivityToolbar from "../../components/activity/ActivityToolbar.svelte";
	import ActivityTable from "../../components/activity/ActivityTable.svelte";
	import LiveStrip from "../../components/activity/LiveStrip.svelte";
	import PendingRow from "../../components/pending/PendingRow.svelte";

	type View = "queue" | "history";

	let view = $state<View>("queue");
	let statusFilter = $state<string[]>([]);
	let search = $state("");
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
			{queueItems.length} active · {historyItems.length} in history
		</p>
	</header>

	{#if auth.isAdmin && (pendingItems.length > 0 || pendingQuery.isError)}
		<section
			class="mb-4 rounded-xl border border-status-wanted/30 bg-status-wanted/[0.04] p-4"
		>
			<div class="mb-3 flex items-center gap-2">
				<h2 class="text-sm font-semibold text-fg">Needs attention</h2>
				{#if pendingItems.length > 0}
					<span
						class="rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
					>
						{pendingItems.length}
					</span>
				{/if}
			</div>
			{#if pendingQuery.isError}
				<p class="text-sm text-status-failed">Failed to load proposals.</p>
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
		</section>
	{/if}

	<LiveStrip items={queueItems} />

	<ActivityToolbar
		{view}
		{statusFilter}
		{search}
		{density}
		clearableCount={historyItems.filter((h) => h.status === "completed").length}
		onViewChange={(v) => {
			view = v;
			statusFilter = [];
		}}
		onStatusFilterChange={(s) => (statusFilter = s)}
		onSearchChange={(q) => (search = q)}
		onDensityToggle={() =>
			(density = density === "comfortable" ? "compact" : "comfortable")}
		onClearCompleted={auth.isAdmin
			? () => clearCompleted.mutate()
			: undefined}
	/>

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
</div>
