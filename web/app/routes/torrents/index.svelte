<script lang="ts">
	import { onMount, untrack } from "svelte";
	import { params } from "@roxi/routify";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { engineDisabled } from "@lib/activity-nav";
	import { auth } from "@lib/auth.svelte";
	import { requireAdmin } from "@lib/guards";
	import { toast } from "@lib/toast";
	import { pullRefresh } from "@lib/pull-refresh";
	import { fold } from "@lib/text";
	import { Selection } from "@lib/selection.svelte";
	import { NOUN_TORRENT } from "@lib/nouns";
	import {
		TORRENT_SORT_CHIPS,
		sortTorrents,
		torrentMatches,
		torrentSortChipKey,
		type SortOrder,
		type TorrentSortKey,
	} from "@lib/activity-touch";
	import type {
		Torrent,
		TorrentList,
		TorrentDetails,
		TorrentAddResult,
		TorrentFilePriority,
		AddTorrentRequest,
	} from "@lib/types";
	import {
		Zap,
		ArrowUpRight,
		LoaderCircle,
		Plus,
		Network,
	} from "@lucide/svelte";
	import ActivityToolbar, {
		ACTIVITY_CHIPS,
	} from "@components/activity/ActivityToolbar.svelte";
	import ActivityFilterSheet from "@components/activity/ActivityFilterSheet.svelte";
	import TouchStatLine from "@components/activity/TouchStatLine.svelte";
	import TorrentTable from "@components/activity/TorrentTable.svelte";
	import TorrentTouchList from "@components/activity/TorrentTouchList.svelte";
	import TorrentDrawer from "@components/activity/TorrentDrawer.svelte";
	import TorrentRemoveDialog from "@components/activity/TorrentRemoveDialog.svelte";
	import AddTorrentModal from "@components/activity/AddTorrentModal.svelte";
	import AddTorrentSheet from "@components/activity/AddTorrentSheet.svelte";
	import TorrentBulkActions from "@components/activity/TorrentBulkActions.svelte";
	import SelectionTopBar from "@components/shared/SelectionTopBar.svelte";
	import { formatSpeed } from "@lib/format";
	import { m as i18n } from "@lib/paraglide/messages.js";

	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	// Moving the peer port is normally gluetun's VPN_PORT_FORWARDING_UP_COMMAND
	// calling this endpoint on every rotation. It has no retry of its own and a
	// partial move persists until the next rotation, so a human needs a way to
	// re-issue it. The endpoint persists nothing — a restart falls back to
	// STREAMLINE_TORRENT_LISTEN_PORT — which is why this lives beside the live
	// engine rather than on a settings form.
	let portOpen = $state(false);
	let portDraft = $state("");

	const setPort = createMutation<null, Error, number>(() => ({
		mutationFn: (port) =>
			api<null>("/torrents/listen-port", { method: "PUT", body: { port } }),
		onSuccess: () => {
			toast.ok(i18n.torrent_port_moved());
			portOpen = false;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	function submitPort() {
		const port = Number(portDraft);
		if (!Number.isInteger(port) || port < 1 || port > 65535) {
			toast.err(i18n.torrent_port_invalid());
			return;
		}
		setPort.mutate(port);
	}

	let statusFilter = $state<string[]>([]);
	let search = $state("");
	// The open drawer is in the URL (?hash=), so it survives a reload and the
	// activity page can link a download straight to its torrent. Seeded from
	// Routify's params, which carry the query string: on an in-app navigation
	// the page mounts before window.location holds the new URL, so reading it
	// here found the page being left and the drawer never opened.
	let selectedHash = $state<string | null>(null);
	onMount(() => {
		let seeded = false;
		return params.subscribe((p) => {
			if (seeded) return;
			seeded = true;
			if (typeof p.hash === "string" && p.hash)
				selectedHash = p.hash.toLowerCase();
		});
	});
	$effect(() => {
		// Never rewrite another page's URL while this one is still mounting.
		if (typeof window === "undefined" || window.location.pathname !== "/torrents")
			return;
		const p = new URLSearchParams(window.location.search);
		if (selectedHash) p.set("hash", selectedHash);
		else p.delete("hash");
		const qs = p.toString();
		const next = `${window.location.pathname}${qs ? `?${qs}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});
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
		enabled: auth.isAdmin,
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
					vars.magnet ? i18n.torrent_magnet_added() : i18n.torrent_added(),
				);
			},
			onError: (e) => toast.err(errorText(e)),
		}),
	);
	const pauseTorrent = createMutation<unknown, Error, string>(() => ({
		mutationFn: (hash) => api(`/torrents/${hash}/pause`, { method: "POST" }),
		onSuccess: invalidateTorrents,
		onError: (e) => toast.err(errorText(e)),
	}));
	const resumeTorrent = createMutation<unknown, Error, string>(() => ({
		mutationFn: (hash) => api(`/torrents/${hash}/resume`, { method: "POST" }),
		onSuccess: invalidateTorrents,
		onError: (e) => toast.err(errorText(e)),
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
			// The server purges the download record tracking this torrent, so the
			// queue on the Activity page is stale the moment this returns.
			qc.invalidateQueries({ queryKey: ["activity", "queue"] });
			selectedHash = null;
			toast.ok(i18n.torrent_removed());
		},
		onError: (e) => toast.err(errorText(e)),
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
		onError: (e) => toast.err(errorText(e)),
	}));

	let torrentItems = $derived<Torrent[]>(torrents.data?.items ?? []);
	let torrentsNotConfigured = $derived(engineDisabled(torrents.error));
	let filteredRows = $derived.by<Torrent[]>(() => {
		let out = torrentItems;
		if (statusFilter.length)
			out = out.filter((t) => statusFilter.includes(t.status));
		if (search.trim()) {
			const q = fold(search);
			out = out.filter((t) => torrentMatches(t, q));
		}
		return out;
	});
	// Sorted once, then handed to both the table and the touch list.
	let torrentRows = $derived(sortTorrents(filteredRows, sort, order));
	// A row swiped away asks before it goes. Held by hash: the list repolls
	// every 2 s, and the dialog names the torrent from the live row.
	let swipeRemoveHash = $state<string | null>(null);
	let swipeRemoveTorrent = $derived(
		swipeRemoveHash === null
			? null
			: (torrentRows.find((t) => t.hash === swipeRemoveHash) ?? null),
	);
	let sortChipKey = $derived(torrentSortChipKey(sort, order));
	let activeFilters = $derived(statusFilter.length + (search.trim() ? 1 : 0));
	let selectedTorrent = $derived(
		torrentItems.find((t) => t.hash === selectedHash) ?? null,
	);
	// A torrent that is not in a fresh listing has nothing to open: a stale
	// link, or one removed elsewhere while its drawer was up. Waiting for a
	// settled fetch keeps a cached list from dropping a hash the refetch on
	// arrival is about to bring in.
	$effect(() => {
		if (
			selectedHash &&
			torrents.isSuccess &&
			!torrents.isFetching &&
			!selectedTorrent
		)
			selectedHash = null;
	});
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

	// ── Selection ────────────────────────────────────────────────────────────
	// Bulk pause / resume / remove. The table carries checkboxes; below md a
	// press and hold on a row starts selecting, and the selection bar takes the
	// toolbar's line.
	const selection = new Selection<string>();
	let selecting = $derived(auth.isAdmin && selection.selecting);
	// A selection made under one filter is meaningless under the next, and the
	// bar would keep acting on rows the user can no longer see.
	$effect(() => {
		statusFilter;
		search;
		untrack(() => selection.clear());
	});
	// A torrent removed elsewhere leaves the list on the next poll; it leaves
	// the selection with it.
	$effect(() => {
		const hashes = torrentItems.map((t) => t.hash);
		untrack(() => selection.prune(hashes));
	});

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
	// only this page knows whether the engine is on. It floats over the end of the
	// list like any corner action: `main`'s pb-16 keeps content clear of the bar,
	// and reserving a second band for the pill on top of that left a visibly empty
	// strip at the bottom of every scroll.
	let showAddPill = $derived(compact && !torrentsNotConfigured && !selecting);
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
			{i18n.common_pull_to_refresh()}
		</span>
		<span class="hidden group-data-[pull-armed]:inline">{i18n.common_release_to_refresh()}</span>
		<span class="hidden group-data-[refreshing]:inline">{i18n.common_refreshing()}</span>
	</div>

	<header class="mb-1 flex flex-wrap items-start justify-between gap-3">
		<div>
			<p class="text-sm text-fg-muted">
				{#if torrentsNotConfigured}
					{i18n.torrent_engine_disabled()}
				{:else}
					{(torrentItems.length === 1
						? i18n.torrent_engine_count_one
						: i18n.torrent_engine_count_other)({ count: torrentItems.length })}
				{/if}
			</p>
		</div>
		{#if !torrentsNotConfigured}
			<div class="flex items-center gap-2">
				{#if portOpen}
					<input
						type="number"
						min="1"
						max="65535"
						inputmode="numeric"
						placeholder="51413"
						bind:value={portDraft}
						onkeydown={(e) => {
							if (e.key === "Enter") submitPort();
							if (e.key === "Escape") portOpen = false;
						}}
						class="h-9 w-28 rounded-md border border-border bg-bg px-3 font-mono text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
					/>
					<button
						type="button"
						disabled={setPort.isPending}
						onclick={submitPort}
						class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md bg-accent px-3 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
					>
						{setPort.isPending ? i18n.common_applying() : i18n.torrent_port_apply()}
					</button>
					<button
						type="button"
						onclick={() => (portOpen = false)}
						class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 text-sm text-fg-muted transition hover:text-fg"
					>
						{i18n.common_cancel()}
					</button>
				{:else}
					<button
						type="button"
						onclick={() => (portOpen = true)}
						title={i18n.torrent_port_help()}
						class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						<Network size={14} aria-hidden="true" />
						{i18n.torrent_port_move()}
					</button>
				{/if}
			</div>
		{/if}
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
					{i18n.torrent_builtin_disabled()}
				</p>
				<p class="mx-auto mt-1 max-w-sm text-xs text-fg-muted">
					{i18n.torrents_empty_help()}
				</p>
			</div>
			<a
				href="/settings/download-clients"
				class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
			>
				{i18n.torrent_enable_in_settings()}
				<ArrowUpRight size={15} aria-hidden="true" />
			</a>
		</div>
	{:else}
		<TouchStatLine
			stats={[
				{ value: String(torrentDownloading), label: i18n.torrent_down() },
				{
					value: String(torrentSeeding),
					label: i18n.status_seeding(),
					color: "var(--status-seeding)",
				},
				{
					value: formatSpeed(torrentAggDown) || "—",
					label: i18n.torrent_agg_down(),
					color: "var(--status-downloading)",
				},
				{
					value: formatSpeed(torrentAggUp) || "—",
					label: i18n.torrent_agg_up(),
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
					{i18n.status_downloading()}
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-seeding">
					{torrentSeeding}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					{i18n.status_seeding()}
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-downloading">
					{formatSpeed(torrentAggDown) || "—"}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					{i18n.torrent_aggregate_down()}
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-seeding">
					{formatSpeed(torrentAggUp) || "—"}
				</div>
				<div
					class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint"
				>
					{i18n.sort_aggregate_up()}
				</div>
			</div>
		</div>

		<!-- Below md an active selection takes over the top bar (SelectionTopBar
		     draws nothing in place), so the toolbar and the list stay put. -->
		{#if selecting}
			<SelectionTopBar
				count={selection.size}
				total={torrentRows.length}
				noun={NOUN_TORRENT}
				onClear={() => selection.clear()}
				onSelectAll={() => selection.selectAll(torrentRows.map((t) => t.hash))}
			/>
		{/if}
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
			selected={auth.isAdmin ? selection.keys : undefined}
			onToggle={(h, v) => selection.toggle(h, v)}
			onToggleAll={(v) =>
				v
					? selection.selectAll(torrentRows.map((t) => t.hash))
					: selection.clear()}
		/>

		<TorrentTouchList
			rows={torrentRows}
			loading={torrents.isPending}
			error={torrents.error ?? null}
			canControl={auth.isAdmin}
			selected={auth.isAdmin ? selection.keys : undefined}
			{selecting}
			onToggle={(h) => selection.toggle(h)}
			onBeginSelect={auth.isAdmin ? (h) => selection.begin(h) : undefined}
			onOpen={(h) => (selectedHash = h)}
			onPause={(h) => pauseTorrent.mutate(h)}
			onResume={(h) => resumeTorrent.mutate(h)}
			onRemove={(t) => (swipeRemoveHash = t.hash)}
		/>

		{#if auth.isAdmin}
			<TorrentBulkActions
				torrents={torrentRows}
				selected={selection.keys}
				onSelectAll={() => selection.selectAll(torrentRows.map((t) => t.hash))}
				onClear={() => selection.clear()}
			/>
		{/if}
	{/if}
</div>

{#if showAddPill}
	<button
		type="button"
		onclick={() => (addOpen = true)}
		class="add-torrent-pill fixed right-4 z-30 flex h-[52px] items-center gap-2 rounded-full bg-accent pl-[17px] pr-5 text-[15px] font-semibold text-fg-on-accent shadow-4 transition active:bg-accent-pressed md:hidden"
	>
		<Plus size={20} strokeWidth={2.4} aria-hidden="true" />
		{i18n.action_add_torrent()}
	</button>
{/if}

<ActivityFilterSheet
	open={filtersOpen}
	onClose={() => (filtersOpen = false)}
	{search}
	onSearchChange={(q) => (search = q)}
	searchPlaceholder={i18n.activity_filter_placeholder()}
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

<TorrentRemoveDialog
	torrent={swipeRemoveTorrent}
	onClose={() => (swipeRemoveHash = null)}
	onRemove={(hash, deleteFiles) => removeTorrent.mutate({ hash, deleteFiles })}
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
