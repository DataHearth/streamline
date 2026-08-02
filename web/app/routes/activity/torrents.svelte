<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import type {
		Torrent,
		TorrentList,
		TorrentDetails,
		TorrentAddResult,
		TorrentFilePriority,
		AddTorrentRequest,
	} from "../../lib/types";
	import ActivityToolbar from "../../components/activity/ActivityToolbar.svelte";
	import TorrentTable from "../../components/activity/TorrentTable.svelte";
	import TorrentDrawer from "../../components/activity/TorrentDrawer.svelte";
	import AddTorrentModal from "../../components/activity/AddTorrentModal.svelte";
	import { formatSpeed } from "../../lib/format";

	let statusFilter = $state<string[]>([]);
	let search = $state("");
	let selectedHash = $state<string | null>(null);
	let addOpen = $state(false);
	const qc = useQueryClient();

	const torrents = createQuery<TorrentList>(() => ({
		queryKey: ["activity", "torrents"],
		queryFn: () => api<TorrentList>("/torrents"),
		refetchInterval: 2000,
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
	let torrentsNotConfigured = $derived(
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
					t.name.toLowerCase().includes(q) || t.hash.toLowerCase().includes(q),
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
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<header class="mb-4">
		<h1 class="text-2xl font-bold tracking-tight text-fg">Torrents</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{torrentItems.length} torrent{torrentItems.length === 1 ? "" : "s"} · built-in
			engine
		</p>
	</header>

	{#if !torrentsNotConfigured}
		<div
			class="mb-4 grid grid-cols-2 gap-4 rounded-lg border border-border bg-bg-elevated px-5 py-4 sm:grid-cols-4"
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
	{/if}

	<ActivityToolbar
		view="torrents"
		{statusFilter}
		{search}
		onStatusFilterChange={(s) => (statusFilter = s)}
		onSearchChange={(q) => (search = q)}
		onAddTorrent={() => (addOpen = true)}
		canAddTorrent={auth.isAdmin && !torrentsNotConfigured}
	/>

	<TorrentTable
		rows={torrentRows}
		loading={torrents.isPending && !torrentsNotConfigured}
		error={torrentsNotConfigured ? null : (torrents.error ?? null)}
		notConfigured={torrentsNotConfigured}
		canControl={auth.isAdmin}
		{selectedHash}
		onOpen={(h) => (selectedHash = h)}
	/>
</div>

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

<AddTorrentModal
	open={addOpen}
	busy={addTorrent.isPending}
	onClose={() => (addOpen = false)}
	onAdd={(payload) => addTorrent.mutate(payload)}
/>
