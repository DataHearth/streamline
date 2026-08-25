<script lang="ts">
	import { slide } from "svelte/transition";
	import { ChevronDown, ChevronRight, LoaderCircle } from "@lucide/svelte";
	import {
		createQuery,
		createInfiniteQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import { pullRefresh } from "../../lib/pull-refresh";
	import { formatEta, formatSpeed } from "../../lib/format";
	import { fold } from "../../lib/text";
	import type {
		ActivityList,
		DownloadQueue,
		DownloadHistory,
		QueueEntry,
		HistoryEntry,
		PendingList,
		PendingItem,
	} from "../../lib/types";
	import ActivityToolbar, {
		ACTIVITY_CHIPS,
	} from "../../components/activity/ActivityToolbar.svelte";
	import ActivityViewSwitch from "../../components/activity/ActivityViewSwitch.svelte";
	import ActivityTable from "../../components/activity/ActivityTable.svelte";
	import ActivityTouchList from "../../components/activity/ActivityTouchList.svelte";
	import ActivityDetailSheet from "../../components/activity/ActivityDetailSheet.svelte";
	import ActivityFilterSheet from "../../components/activity/ActivityFilterSheet.svelte";
	import PendingSheet from "../../components/activity/PendingSheet.svelte";
	import ResolveDialog from "../../components/activity/ResolveDialog.svelte";
	import TouchStatLine from "../../components/activity/TouchStatLine.svelte";
	import LiveStrip from "../../components/activity/LiveStrip.svelte";
	import PendingRow from "../../components/pending/PendingRow.svelte";
	import EventList from "../../components/activity/EventList.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Torrents live on their own route (/activity/torrents); this page is the
	// queue/history/events trio the switch above the toolbar swaps between.
	type View = "queue" | "history" | "events";

	// The switch is page state, not a route, but ?view is honoured — the
	// dashboard's Recent activity panel links straight to the events feed it
	// samples, which used to land on the queue instead.
	function initialView(): View {
		if (typeof window === "undefined") return "queue";
		const v = new URLSearchParams(window.location.search).get("view");
		return v === "history" || v === "events" ? v : "queue";
	}

	let view = $state<View>(initialView());
	let statusFilter = $state<string[]>([]);
	let search = $state("");
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

	const events = createInfiniteQuery<
		ActivityList,
		Error,
		{ pages: ActivityList[]; pageParams: (string | null)[] },
		readonly ["activity", "events"],
		string | null
	>(() => ({
		queryKey: ["activity", "events"] as const,
		queryFn: ({ pageParam }) => {
			const p = new URLSearchParams({ limit: String(PAGE) });
			if (pageParam) p.set("cursor", pageParam);
			return api<ActivityList>(`/activity?${p.toString()}`);
		},
		initialPageParam: null,
		getNextPageParam: (last) => last.next_cursor ?? undefined,
		enabled: view === "events",
	}));

	let eventItems = $derived(
		(events.data?.pages ?? []).flatMap((p) => p.events),
	);

	function switchView(v: View) {
		view = v;
		statusFilter = [];
		detailId = null;
	}

	// Same shape as ActivityTable's: the sentinel mounts and unmounts with the
	// view, so the effect keys on the binding to re-observe, and reads the query
	// flags inside the callback to keep them out of its dependencies.
	let eventSentinel = $state<HTMLDivElement | null>(null);
	$effect(() => {
		const el = eventSentinel;
		if (!el) return;
		const io = new IntersectionObserver((entries) => {
			if (
				entries[0]?.isIntersecting &&
				events.hasNextPage &&
				!events.isFetchingNextPage
			) {
				events.fetchNextPage();
			}
		});
		io.observe(el);
		return () => io.disconnect();
	});

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
		onError: (e) => toast.err(errorText(e)),
	}));
	const pause = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) => api(`/activity/queue/${id}/pause`, { method: "POST" }),
		onSuccess: invalidate,
		onError: (e) => toast.err(errorText(e)),
	}));
	const resume = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) => api(`/activity/queue/${id}/resume`, { method: "POST" }),
		onSuccess: invalidate,
		onError: (e) => toast.err(errorText(e)),
	}));
	// A held download is waiting on a person, so the dialog tracks an id: the
	// queue repolls every 2 s and holding the object would show a stale finding
	// list after a refetch. heldItem itself is declared below queueItems, which
	// it reads.
	let resolveId = $state<number | null>(null);

	const resolve = createMutation<
		unknown,
		Error,
		{ id: number; action: "import" | "regrab" | "delete" }
	>(() => ({
		mutationFn: ({ id, action }) =>
			api(`/downloads/${id}/resolve`, { method: "POST", body: { action } }),
		onSuccess: (_r, { action }) => {
			invalidate();
			resolveId = null;
			toast.ok(
				action === "import"
					? i18n.hold_imported()
					: action === "regrab"
						? i18n.hold_searching_again()
						: i18n.hold_deleted(),
			);
		},
		onError: (e) => toast.err(errorText(e)),
	}));

	const removeHistory = createMutation<unknown, Error, number>(() => ({
		mutationFn: (id) => api(`/activity/history/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			toast.ok("Removed");
			invalidate();
		},
		onError: (e) => toast.err(errorText(e)),
	}));
	const clearCompleted = createMutation<unknown, Error, void>(() => ({
		mutationFn: () =>
			api("/activity/history/clear-completed", { method: "POST" }),
		onSuccess: () => {
			toast.ok("Cleared completed");
			invalidate();
		},
		onError: (e) => toast.err(errorText(e)),
	}));

	// "Needs attention": adopted-torrent proposals awaiting a decision (admin).
	const pendingQuery = createQuery<PendingList>(() => ({
		queryKey: ["activity", "pending"],
		queryFn: () => api<PendingList>("/activity/pending"),
		enabled: auth.isAdmin,
		refetchInterval: 30000,
	}));
	let pendingItems = $derived<PendingItem[]>(pendingQuery.data?.items ?? []);
	let showPending = $derived(
		auth.isAdmin && (pendingItems.length > 0 || pendingQuery.isError),
	);
	let attnOpen = $state(true);
	// Below md the section is one banner line and the proposals open in a sheet:
	// three of them expanded push the queue entirely below the fold, and each one's
	// three decisions want the full width.
	let pendingOpen = $state(false);

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
		onError: (e) => toast.err(errorText(e)),
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
		onError: (e) => toast.err(errorText(e)),
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
		onError: (e) => toast.err(errorText(e)),
	}));

	let pendingBusyId = $derived.by<number | null>(() => {
		if (importPending.isPending) return importPending.variables ?? null;
		if (replacePending.isPending) return replacePending.variables?.id ?? null;
		if (ignorePending.isPending) return ignorePending.variables?.id ?? null;
		return null;
	});

	// The table, its sheets and the toolbar only ever deal in download records;
	// the events feed is its own branch below and never reaches them.
	let tableView = $derived<"queue" | "history">(
		view === "history" ? "history" : "queue",
	);

	let queueItems = $derived<QueueEntry[]>(queue.data?.items ?? []);
	let heldItem = $derived(
		queueItems.find((q) => q.id === resolveId) ?? null,
	);
	let historyItems = $derived<HistoryEntry[]>(
		(history.data?.pages ?? []).flatMap((p) => p.items),
	);

	let source = $derived<(QueueEntry | HistoryEntry)[]>(
		view === "queue" ? queueItems : historyItems,
	);
	let rows = $derived.by<(QueueEntry | HistoryEntry)[]>(() => {
		let out = source;
		if (statusFilter.length)
			out = out.filter((i) => statusFilter.includes(i.status));
		if (search.trim()) {
			const q = fold(search);
			out = out.filter(
				(i) =>
					fold(i.title).includes(q) ||
					fold(i.movie.title).includes(q) ||
					fold(i.episode?.show_title ?? "").includes(q),
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

	// ── Touch surfaces ───────────────────────────────────────────────────────
	let filtersOpen = $state(false);
	let detailId = $state<number | null>(null);
	// Held by id, not by value: the queue repolls every 2 s, and a sheet holding
	// the snapshot it opened with would show a frozen speed and percentage.
	let detailItem = $derived<QueueEntry | HistoryEntry | null>(
		detailId === null ? null : (source.find((i) => i.id === detailId) ?? null),
	);
	let activeFilters = $derived(statusFilter.length + (search.trim() ? 1 : 0));
	let clearableCount = $derived(
		historyItems.filter((h) => h.status === "completed").length,
	);

	let aggregate = $derived(
		queueItems.reduce((s, i) => s + (i.download_speed ?? 0), 0),
	);
	let minEta = $derived.by(() => {
		const etas = queueItems.map((i) => i.eta ?? 0).filter((e) => e > 0);
		return etas.length ? Math.min(...etas) : 0;
	});
	let activeCount = $derived(
		queueItems.filter((i) => i.status === "downloading").length,
	);
	// Its own figure, not part of "active": a held download is not transferring,
	// and folding it in would make the number mean two things.
	let heldCount = $derived(
		queueItems.filter((i) => i.status === "held").length,
	);

	function resetFilters() {
		statusFilter = [];
		search = "";
	}

	// A pull re-fetches everything the page shows, not just the view in front —
	// the point of the gesture is "is all of this current?".
	async function refreshAll() {
		await Promise.all([
			qc.refetchQueries({ queryKey: ["activity", "queue"] }),
			qc.refetchQueries({ queryKey: ["activity", "history"] }),
			auth.isAdmin
				? qc.refetchQueries({ queryKey: ["activity", "pending"] })
				: Promise.resolve(),
		]);
	}
</script>

<div
	use:pullRefresh={{ onRefresh: refreshAll }}
	class="group relative flex flex-col px-4 py-6 md:px-6"
>
	<!-- Sits in the gap the pull opens above the page. -->
	<div
		aria-hidden="true"
		class="pointer-events-none absolute inset-x-0 -top-11 flex h-11 items-center justify-center gap-2 text-[11.5px] text-fg-subtle opacity-0 transition-opacity group-data-[pulling]:opacity-100 md:hidden"
	>
		<LoaderCircle
			size={15}
			class="group-data-[refreshing]:motion-safe:animate-spin"
			aria-hidden="true"
		/>
		<span class="group-data-[pull-armed]:hidden group-data-[refreshing]:hidden">
			{i18n.common_pull_to_refresh()}
		</span>
		<span class="hidden group-data-[pull-armed]:inline">{i18n.common_release_to_refresh()}</span>
		<span class="hidden group-data-[refreshing]:inline">{i18n.common_refreshing()}</span>
	</div>

	<header class="mb-1">
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{view === "events"
				? i18n.activity_events()
				: i18n.activity_queue_and_history()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{#if view === "events"}
				{i18n.activity_events_subtitle()}
			{:else}
				{queueItems.length} active · {historyItems.length} in history
			{/if}
		</p>
	</header>

	{#if showPending}
		<section
			class="mt-4 hidden rounded-xl border border-status-wanted/30 bg-status-wanted/[0.04] p-4 md:block"
		>
			<button
				type="button"
				onclick={() => (attnOpen = !attnOpen)}
				aria-expanded={attnOpen}
				class="flex w-full items-center gap-2 text-left {attnOpen ? 'mb-3' : ''}"
			>
				<h2 class="text-sm font-semibold text-fg">{i18n.common_needs_attention()}</h2>
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
						<p class="text-sm text-status-failed">{i18n.torrent_proposals_failed()}</p>
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

	<div class="hidden md:mt-3 md:block">
		<LiveStrip items={queueItems} />
	</div>

	<TouchStatLine
		stats={[
			{ value: String(activeCount), label: i18n.common_active() },
			...(heldCount > 0
				? [
						{
							value: String(heldCount),
							label: i18n.status_held(),
							color: "var(--status-held)",
						},
					]
				: []),
			{
				value: formatSpeed(aggregate) || "—",
				label: i18n.torrent_aggregate_down(),
				color: "var(--status-downloading)",
			},
			{ value: formatEta(minEta) || "—", label: i18n.activity_next_eta() },
		]}
	/>

	{#if view === "events"}
		<!-- The events feed is the dashboard panel's "View all" target: media
		     events across the library, not download records, so it carries no
		     status chips and shares only the view switch. -->
		<div class="mt-2 flex items-center gap-2">
			<ActivityViewSwitch
				{view}
				counts={{ queue: queueItems.length, history: historyItems.length }}
				onViewChange={switchView}
			/>
		</div>

		<div
			class="mt-3 overflow-hidden rounded-lg border border-border bg-bg-elevated"
		>
			{#if events.isPending}
				<p class="px-5 py-10 text-center text-sm text-fg-subtle">
					{i18n.common_loading()}
				</p>
			{:else if events.isError}
				<p class="px-5 py-10 text-center text-sm text-status-failed">
					{errorText(events.error)}
				</p>
			{:else if eventItems.length === 0}
				<p class="px-5 py-10 text-center text-sm text-fg-muted">
					{i18n.activity_events_empty()}
				</p>
			{:else}
				<EventList events={eventItems} />
				{#if events.hasNextPage}
					<div bind:this={eventSentinel} class="h-10">
						{#if events.isFetchingNextPage}
							<p class="py-3 text-center text-xs text-fg-subtle">
								{i18n.common_loading()}
							</p>
						{/if}
					</div>
				{/if}
			{/if}
		</div>
	{:else}
	<ActivityToolbar
		view={tableView}
		{statusFilter}
		{search}
		{activeFilters}
		{clearableCount}
		onOpenFilters={() => (filtersOpen = true)}
		onStatusFilterChange={(s) => (statusFilter = s)}
		onSearchChange={(q) => (search = q)}
		onClearCompleted={auth.isAdmin ? () => clearCompleted.mutate() : undefined}
	>
		{#snippet leading()}
			<ActivityViewSwitch
				{view}
				counts={{ queue: queueItems.length, history: historyItems.length }}
				onViewChange={switchView}
			/>
		{/snippet}
	</ActivityToolbar>

	{#if showPending}
		<!-- Below md the section is one line, and it sits under the toolbar rather than
		     above the stats: up there it pushed the stat card down, so the same numbers
		     landed in a different place on Queue than on Torrents. -->
		<button
			type="button"
			onclick={() => (pendingOpen = true)}
			class="mt-2 flex items-center gap-2.5 rounded-xl border border-status-wanted/30 bg-status-wanted/[0.06] px-3.5 py-2.5 text-left md:hidden"
		>
			<span
				class="h-[7px] w-[7px] shrink-0 rounded-full bg-status-wanted"
				aria-hidden="true"
			></span>
			<span class="min-w-0 flex-1 truncate text-[12.5px] font-medium text-fg">
				{#if pendingQuery.isError}
					Couldn't load proposals
				{:else}
					{pendingItems.length}
					{pendingItems.length === 1 ? "proposal needs" : "proposals need"} a decision
				{/if}
			</span>
			<span class="shrink-0 text-[12.5px] font-semibold text-status-wanted">
				{i18n.common_review()}
			</span>
			<ChevronRight size={14} class="shrink-0 text-status-wanted" aria-hidden="true" />
		</button>
	{/if}

	<ActivityTable
		view={tableView}
		{rows}
		{busyId}
		loading={view === "queue" ? queue.isPending : history.isPending}
		error={view === "queue" ? (queue.error ?? null) : (history.error ?? null)}
		hasMore={view === "history" && (history.hasNextPage ?? false)}
		loadingMore={history.isFetchingNextPage}
		canControl={auth.isAdmin}
		onLoadMore={() => history.fetchNextPage()}
		onCancel={(id) => cancel.mutate(id)}
		onPause={(id) => pause.mutate(id)}
		onResume={(id) => resume.mutate(id)}
		onRemove={(id) => removeHistory.mutate(id)}
		onResolve={auth.isAdmin ? (item) => (resolveId = item.id) : undefined}
	/>

	<ActivityTouchList
		view={tableView}
		{rows}
		loading={view === "queue" ? queue.isPending : history.isPending}
		error={view === "queue" ? (queue.error ?? null) : (history.error ?? null)}
		hasMore={view === "history" && (history.hasNextPage ?? false)}
		loadingMore={history.isFetchingNextPage}
		onLoadMore={() => history.fetchNextPage()}
		onOpen={(item) => (detailId = item.id)}
		onResolve={auth.isAdmin ? (item) => (resolveId = item.id) : undefined}
	/>
	{/if}
</div>

<ActivityFilterSheet
	open={filtersOpen}
	onClose={() => (filtersOpen = false)}
	{search}
	onSearchChange={(q) => (search = q)}
	searchPlaceholder="Filter title or movie…"
	onReset={resetFilters}
	activeCount={activeFilters}
/>

<PendingSheet
	open={pendingOpen}
	items={pendingItems}
	busyId={pendingBusyId}
	error={pendingQuery.isError}
	onClose={() => (pendingOpen = false)}
	onImport={(id) => importPending.mutate(id)}
	onReplace={(id, removeOld) => replacePending.mutate({ id, removeOld })}
	onIgnore={(id, removeTorrent) => ignorePending.mutate({ id, removeTorrent })}
/>

<ResolveDialog
	item={heldItem}
	pending={resolve.isPending}
	onResolve={(action) =>
		resolveId !== null && resolve.mutate({ id: resolveId, action })}
	onClose={() => (resolveId = null)}
/>

<ActivityDetailSheet
	item={detailItem}
	view={tableView}
	busy={detailId !== null && busyId === detailId}
	canControl={auth.isAdmin}
	onClose={() => (detailId = null)}
	onCancel={(id) => cancel.mutate(id)}
	onPause={(id) => pause.mutate(id)}
	onResume={(id) => resume.mutate(id)}
	onRemove={(id) => removeHistory.mutate(id)}
/>
