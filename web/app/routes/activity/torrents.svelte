<script lang="ts">
	import { onMount } from "svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { engineDisabled } from "../../lib/activity-nav";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import { pullRefresh } from "../../lib/pull-refresh";
	import {
		TORRENT_SORT_CHIPS,
		sortTorrents,
		torrentSortChipKey,
		type SortOrder,
		type TorrentSortKey,
	} from "../../lib/activity-touch";
	import type {
		Torrent,
		TorrentList,
		TorrentDetails,
		TorrentAddResult,
		TorrentFilePriority,
		AddTorrentRequest,
	} from "../../lib/types";
	import { Zap, ArrowUpRight, LoaderCircle, Plus } from "@lucide/svelte";
	import ActivityToolbar, {
		ACTIVITY_CHIPS,
	} from "../../components/activity/ActivityToolbar.svelte";
	import ActivityFilterSheet from "../../components/activity/ActivityFilterSheet.svelte";
	import TouchStatLine from "../../components/activity/TouchStatLine.svelte";
	import TorrentTable from "../../components/activity/TorrentTable.svelte";
	import TorrentTouchList from "../../components/activity/TorrentTouchList.svelte";
	import TorrentDrawer from "../../components/activity/TorrentDrawer.svelte";
	import AddTorrentModal from "../../components/activity/AddTorrentModal.svelte";
	import AddTorrentSheet from "../../components/activity/AddTorrentSheet.svelte";
	import { formatSpeed } from "../../lib/format";

	let statusFilter = $state<string[]>([]);
	let search = $state("");
	let selectedHash = $state<string | null>(null);
	let addOpen = $state(false);
	let filtersOpen = $state(false);
	// One sort for both surfaces — the table's headers set it from md up, the
	// filter sheet's chips below lg. Held here so the two can't disagree.
	let sort = $state<TorrentSortKey>("status");
	let order = $state<SortOrder>("asc");
	const qc = useQueryClient();

	// Below md the add form is a sheet rather than the centred modal — different
	// components, not something CSS can pick. Same switch AppShell runs for the
	// library's add flow.
	let compact = $state(false);
	onMount(() => {
		const mql = window.matchMedia("(max-width: 767px)");
		const sync = () => (compact = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});

	const torrents = createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		retry: (count, e) => !engineDisabled(e) && count < 1,
		refetchInterval: (q) => (engineDisabled(q.state.error) ? false : 2000),
		refetchOnWindowFocus: (q) => !engineDisabled(q.state.error),
	}));
	// The list stays light — files / peers / trackers come from a per-torrent
	// detail query that polls only while the drawer is open (2 s), per the
	// reconciled contract.
	const torrentDetail = createQuery<TorrentDetails>(() => ({
		queryKey: ["torrents", "detail", selectedHash],
		queryFn: () => api<TorrentDetails>(`/torrents/${selectedHash}`),
		enabled: !!selectedHash,
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
					vars.magnet ? "Magnet added — fetching metadata" : "Torrent added",
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
	let torrentsNotConfigured = $derived(engineDisabled(torrents.error));
	let filteredRows = $derived.by<Torrent[]>(() => {
		let out = torrentItems;
		if (statusFilter.length)
			out = out.filter((t) => statusFilter.includes(t.status));
		if (search.trim()) {
			const q = search.toLowerCase();
			out = out.filter(
				(t) =>
					t.name.toLowerCase().includes(q) || t.hash.toLowerCase().includes(q),
			);
		}
		return out;
	});
	// Sorted once, then handed to both the table and the touch list.
	let torrentRows = $derived(sortTorrents(filteredRows, sort, order));
	let sortChipKey = $derived(torrentSortChipKey(sort, order));
	let activeFilters = $derived(statusFilter.length + (search.trim() ? 1 : 0));
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

	function resetFilters() {
		statusFilter = [];
		search = "";
	}

	function pickSortChip(key: string) {
		const chip = TORRENT_SORT_CHIPS.find((c) => c.key === key);
		if (!chip) return;
		sort = chip.sort;
		order = chip.order;
	}

	async function refreshAll() {
		if (torrentsNotConfigured) return;
		await qc.refetchQueries({ queryKey: ["activity", "torrents"] });
	}

	// Adding is a corner pill above the bottom nav below md, the same way the
	// library routes do it — a button in the toolbar line is at the far end of the
	// screen from a thumb. AddButton owns the library's; this owns its own, because
	// only this page knows whether the engine is on. The clearance flag is the one
	// AppShell already handles: main's pb-16 clears the bar, not a pill floating
	// above it.
	let showAddPill = $derived(compact && !torrentsNotConfigured && auth.isAdmin);
	$effect(() => {
		if (!showAddPill) return;
		document.body.dataset.addPill = "";
		return () => {
			delete document.body.dataset.addPill;
		};
	});
</script>

<div
	use:pullRefresh={{ onRefresh: refreshAll, disabled: torrentsNotConfigured }}
	class="group relative flex flex-col px-4 py-6 md:px-6"
>
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
			Pull to refresh
		</span>
		<span class="hidden group-data-[pull-armed]:inline">Release to refresh</span>
		<span class="hidden group-data-[refreshing]:inline">Refreshing…</span>
	</div>

	<header class="mb-1">
		<h1 class="text-2xl font-bold tracking-tight text-fg">Torrents</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{#if torrentsNotConfigured}
				Built-in engine · disabled
			{:else}
				{torrentItems.length} torrent{torrentItems.length === 1 ? "" : "s"} · built-in
				engine
			{/if}
		</p>
	</header>

	{#if torrentsNotConfigured}
		<div
			class="mt-4 flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-5 py-14 text-center md:py-20"
		>
			<div
				class="grid h-12 w-12 place-items-center rounded-full bg-accent-soft text-accent"
			>
				<Zap size={22} aria-hidden="true" />
			</div>
			<div>
				<p class="text-sm font-semibold text-fg">
					The built-in client isn’t enabled
				</p>
				<p class="mx-auto mt-1 max-w-sm text-xs text-fg-muted">
					Enable Streamline’s built-in BitTorrent engine to add and manage
					torrents from here.
				</p>
			</div>
			{#if auth.isAdmin}
				<a
					href="/settings/download-clients"
					class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
				>
					Enable in Settings
					<ArrowUpRight size={15} aria-hidden="true" />
				</a>
			{:else}
				<p class="text-xs text-fg-subtle">Ask an admin to enable it in Settings.</p>
			{/if}
		</div>
	{:else}
		<TouchStatLine
			stats={[
				{ value: String(torrentDownloading), label: "Down" },
				{
					value: String(torrentSeeding),
					label: "Seeding",
					color: "var(--status-seeding)",
				},
				{
					value: formatSpeed(torrentAggDown) || "—",
					label: "Agg ↓",
					color: "var(--status-downloading)",
				},
				{
					value: formatSpeed(torrentAggUp) || "—",
					label: "Agg ↑",
					color: "var(--status-seeding)",
				},
			]}
		/>

		<div
			class="mb-4 mt-3 hidden grid-cols-2 gap-4 rounded-lg border border-border bg-bg-elevated px-5 py-4 sm:grid-cols-4 md:grid"
		>
			<div>
				<div class="text-2xl font-bold tabular-nums text-fg">
					{torrentDownloading}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					Downloading
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-seeding">
					{torrentSeeding}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					Seeding
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-downloading">
					{formatSpeed(torrentAggDown) || "—"}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					Aggregate ↓
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-seeding">
					{formatSpeed(torrentAggUp) || "—"}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					Aggregate ↑
				</div>
			</div>
		</div>

		<ActivityToolbar
			view="torrents"
			{statusFilter}
			{search}
			{activeFilters}
			onOpenFilters={() => (filtersOpen = true)}
			onStatusFilterChange={(s) => (statusFilter = s)}
			onSearchChange={(q) => (search = q)}
			onAddTorrent={() => (addOpen = true)}
			canAddTorrent={auth.isAdmin}
		/>

		<TorrentTable
			rows={torrentRows}
			loading={torrents.isPending}
			error={torrents.error ?? null}
			canControl={auth.isAdmin}
			{selectedHash}
			{sort}
			{order}
			onSortChange={(s, o) => {
				sort = s;
				order = o;
			}}
			onOpen={(h) => (selectedHash = h)}
		/>

		<TorrentTouchList
			rows={torrentRows}
			loading={torrents.isPending}
			error={torrents.error ?? null}
			canControl={auth.isAdmin}
			onOpen={(h) => (selectedHash = h)}
		/>
	{/if}
</div>

{#if showAddPill}
	<button
		type="button"
		onclick={() => (addOpen = true)}
		class="add-torrent-pill fixed right-4 z-30 flex h-[52px] items-center gap-2 rounded-full bg-accent pl-[17px] pr-5 text-[15px] font-semibold text-fg-on-accent shadow-4 transition active:bg-accent-pressed md:hidden"
	>
		<Plus size={20} strokeWidth={2.4} aria-hidden="true" />
		Add torrent
	</button>
{/if}

<ActivityFilterSheet
	open={filtersOpen}
	onClose={() => (filtersOpen = false)}
	{search}
	onSearchChange={(q) => (search = q)}
	searchPlaceholder="Filter name or hash…"
	sortChips={TORRENT_SORT_CHIPS}
	sortKey={sortChipKey}
	onSortChange={pickSortChip}
	onReset={resetFilters}
	activeCount={activeFilters}
/>

<TorrentDrawer
	open={!!selectedTorrent}
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

{#if compact}
	<AddTorrentSheet
		open={addOpen}
		busy={addTorrent.isPending}
		onClose={() => (addOpen = false)}
		onAdd={(payload) => addTorrent.mutate(payload)}
	/>
{:else}
	<AddTorrentModal
		open={addOpen}
		busy={addTorrent.isPending}
		onClose={() => (addOpen = false)}
		onAdd={(payload) => addTorrent.mutate(payload)}
	/>
{/if}

<style>
	/* Clears the bottom bar (56px + safe area) with 28px of air under the pill —
	   the same offset AddButton uses on the library routes. */
	.add-torrent-pill {
		bottom: calc(env(safe-area-inset-bottom) + 5.25rem);
	}
</style>
