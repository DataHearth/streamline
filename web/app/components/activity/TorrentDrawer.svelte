<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import {
		X,
		Copy,
		Check,
		Pause,
		Play,
		Trash2,
		LoaderCircle,
		FileText,
		Users,
		Globe,
	} from "@lucide/svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import TorrentFilesTab from "./TorrentFilesTab.svelte";
	import TorrentPeersTab from "./TorrentPeersTab.svelte";
	import TorrentTrackersTab from "./TorrentTrackersTab.svelte";
	import { cn } from "../../lib/cn";
	import {
		formatBytes,
		formatSpeed,
		formatEta,
		formatRatio,
	} from "../../lib/format";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type {
		Torrent,
		TorrentDetails,
		TorrentFilePriority,
	} from "../../lib/types";

	let {
		open,
		torrent,
		detail,
		canControl = false,
		busy = false,
		busyFileIndex = null,
		onClose,
		onPause,
		onResume,
		onRemove,
		onSetPriority,
	}: {
		open: boolean;
		torrent: Torrent | null;
		detail: TorrentDetails | null;
		canControl?: boolean;
		busy?: boolean;
		busyFileIndex?: number | null;
		onClose: () => void;
		onPause: (hash: string) => void;
		onResume: (hash: string) => void;
		onRemove: (hash: string, deleteFiles: boolean) => void;
		onSetPriority: (hash: string, index: number, priority: TorrentFilePriority) => void;
	} = $props();

	type Tab = "files" | "peers" | "trackers";
	let tab = $state<Tab>("files");
	let copied = $state(false);
	let confirmRemove = $state(false);
	let deleteFiles = $state(false);

	// Reset to Files + clear transient UI whenever a different torrent opens.
	let lastHash = "";
	$effect(() => {
		if (torrent && torrent.hash !== lastHash) {
			lastHash = torrent.hash;
			tab = "files";
			copied = false;
		}
	});

	$effect(() => {
		if (open) {
			document.body.style.overflow = "hidden";
			return () => {
				document.body.style.overflow = "";
			};
		}
	});

	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy() { node.parentNode?.removeChild(node); } };
	}

	async function copyHash() {
		if (!torrent) return;
		try {
			await navigator.clipboard.writeText(torrent.hash);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			/* clipboard blocked */
		}
	}

	let fetching = $derived(torrent?.status === "fetching");
	let canPause = $derived(
		torrent && (torrent.status === "downloading" || torrent.status === "seeding" || torrent.status === "fetching" || torrent.status === "stalled"),
	);
	let active = $derived(
		torrent && (torrent.status === "downloading" || torrent.status === "seeding" || torrent.status === "fetching"),
	);

	const TABS: { key: Tab; label: string }[] = [
		{ key: "files", label: "Files" },
		{ key: "peers", label: "Peers" },
		{ key: "trackers", label: "Trackers" },
	];
	function tabCount(k: Tab): number {
		if (!detail) return 0;
		if (k === "files") return detail.files.length;
		if (k === "peers") return detail.peers.length;
		return detail.trackers.length;
	}
</script>

{#if open && torrent}
	<div use:portal class="fixed inset-0 z-40" role="presentation">
		<div
			class="absolute inset-0 bg-black/50 backdrop-blur-[2px]"
			transition:fade={{ duration: 160 }}
			onmousedown={onClose}
			role="presentation"
		></div>

		<div
			transition:fly={{ x: 540, duration: 240, easing: cubicOut }}
			class="absolute inset-y-0 right-0 flex w-full max-w-[720px] flex-col border-l border-border bg-bg-elevated shadow-4"
			role="dialog"
			aria-modal="true"
			aria-label="Torrent detail"
		>
			<!-- Header -->
			<header class="shrink-0 border-b border-border p-5">
				<div class="flex items-start justify-between gap-3">
					<div class="flex flex-wrap items-center gap-2">
						<StatusPill status={torrent.status} live={active} />
						{#if torrent.seeding_stopped}
							<span
								class="inline-flex items-center gap-1 rounded-full border border-status-completed/40 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-completed"
								title="Ratio / seed-time limit reached"
							>
								Seeding stopped
							</span>
						{/if}
						{#if !torrent.tracked}
							<span
								class="inline-flex items-center rounded-full border border-border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-subtle"
							>
								untracked
							</span>
						{/if}
					</div>
					<button
						type="button"
						onclick={onClose}
						aria-label="Close"
						class="grid h-8 w-8 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						<X size={16} aria-hidden="true" />
					</button>
				</div>

				<h2 class="mt-3 break-words text-lg font-semibold leading-snug text-fg">
					{#if fetching && !torrent.name}
						<span class="italic text-fg-muted">Fetching metadata…</span>
					{:else}
						{torrent.name}
					{/if}
				</h2>

				<!-- infohash · click to copy -->
				<button
					type="button"
					onclick={copyHash}
					class="group mt-1.5 inline-flex max-w-full items-center gap-1.5 text-left"
					title="Copy infohash"
				>
					<span class="truncate font-mono text-[11px] text-fg-subtle group-hover:text-fg-muted">
						{torrent.hash}
					</span>
					{#if copied}
						<Check size={12} class="shrink-0 text-status-available" aria-hidden="true" />
					{:else}
						<Copy size={12} class="shrink-0 text-fg-faint group-hover:text-fg-muted" aria-hidden="true" />
					{/if}
				</button>

				<!-- overall progress -->
				<div class="mt-4">
					<ProgressBar
						value={fetching ? undefined : torrent.progress}
						status={torrent.status}
						height={4}
						shimmer={torrent.status === "downloading"}
					/>
					<div class="mt-1.5 flex items-center justify-between text-xs">
						<span class="font-mono tabular-nums font-semibold text-fg">
							{fetching ? "—" : `${Math.round(torrent.progress * 100)}%`}
						</span>
						<span class="font-mono tabular-nums text-fg-faint">
							{#if torrent.status === "downloading" && torrent.eta > 0}
								{formatEta(torrent.eta)} left
							{:else if torrent.status === "seeding"}
								seeding
							{:else if torrent.status === "completed"}
								complete
							{/if}
						</span>
					</div>
				</div>

				<!-- stat tiles -->
				<div class="mt-4 grid grid-cols-4 gap-px overflow-hidden rounded-md border border-border bg-border">
					{@render stat("Ratio", fetching ? "—" : formatRatio(torrent.ratio))}
					{@render stat("↓ Down", formatSpeed(torrent.download_speed) || "—")}
					{@render stat("↑ Up", formatSpeed(torrent.upload_speed) || "—")}
					{@render stat("Size", formatBytes(torrent.size))}
				</div>

				<!-- meta -->
				<dl class="mt-4 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-xs">
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">Save path</dt>
					<dd class="min-w-0 break-all font-mono text-fg-muted">{torrent.save_path}</dd>
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">Swarm</dt>
					<dd class="font-mono tabular-nums text-fg-muted">
						{torrent.seeds} seeds · {torrent.peer_count} peers
					</dd>
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">Added</dt>
					<dd class="text-fg-muted" title={formatDateTime(torrent.added_at)}>
						{formatRelative(torrent.added_at)}
					</dd>
				</dl>
			</header>

			<!-- Tabs -->
			<div class="flex shrink-0 items-center gap-1 border-b border-border px-3" role="tablist">
				{#each TABS as t (t.key)}
					{@const activeTab = tab === t.key}
					<button
						type="button"
						role="tab"
						aria-selected={activeTab}
						onclick={() => (tab = t.key)}
						class={cn(
							"relative flex items-center gap-1.5 px-3 py-2.5 text-[13px] font-medium transition",
							activeTab
								? "text-fg after:absolute after:inset-x-3 after:-bottom-px after:h-0.5 after:bg-accent"
								: "text-fg-subtle hover:text-fg",
						)}
					>
						{#if t.key === "files"}<FileText size={13} aria-hidden="true" />{/if}
						{#if t.key === "peers"}<Users size={13} aria-hidden="true" />{/if}
						{#if t.key === "trackers"}<Globe size={13} aria-hidden="true" />{/if}
						{t.label}
						<span class="font-mono tabular-nums text-[10px] text-fg-faint">{tabCount(t.key)}</span>
					</button>
				{/each}
			</div>

			<!-- Tab body -->
			<div class="min-h-0 flex-1 overflow-y-auto p-4">
				{#if !detail}
					<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
						<LoaderCircle size={22} class="text-fg-faint motion-safe:animate-spin" aria-hidden="true" />
						<p class="text-sm text-fg-muted">Loading details…</p>
					</div>
				{:else if tab === "files"}
					{#if detail.files.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
							<FileText size={24} class="text-fg-faint" aria-hidden="true" />
							<p class="text-sm font-medium text-fg">Waiting for metadata</p>
							<p class="text-xs text-fg-muted">
								The file list appears once the magnet resolves.
							</p>
						</div>
					{:else}
						<TorrentFilesTab
							files={detail.files}
							status={torrent.status}
							{canControl}
							busyIndex={busyFileIndex}
							onSetPriority={(index, priority) => onSetPriority(torrent.hash, index, priority)}
						/>
					{/if}
				{:else if tab === "peers"}
					<TorrentPeersTab peers={detail.peers} peerCount={detail.peer_count} status={torrent.status} />
				{:else}
					<TorrentTrackersTab trackers={detail.trackers} />
				{/if}
			</div>

			<!-- Actions -->
			{#if canControl}
				<footer class="flex shrink-0 items-center gap-2 border-t border-border p-4">
					{#if torrent.status === "paused"}
						<button
							type="button"
							disabled={busy}
							onclick={() => onResume(torrent.hash)}
							class="inline-flex h-9 items-center gap-1.5 rounded-md bg-bg-subtle px-3.5 text-sm font-semibold text-fg transition hover:bg-surface disabled:opacity-50"
						>
							{#if busy}<LoaderCircle size={14} class="motion-safe:animate-spin" aria-hidden="true" />{:else}<Play size={14} aria-hidden="true" />{/if}
							Resume
						</button>
					{:else if canPause}
						<button
							type="button"
							disabled={busy}
							onclick={() => onPause(torrent.hash)}
							class="inline-flex h-9 items-center gap-1.5 rounded-md bg-bg-subtle px-3.5 text-sm font-semibold text-fg transition hover:bg-surface disabled:opacity-50"
						>
							{#if busy}<LoaderCircle size={14} class="motion-safe:animate-spin" aria-hidden="true" />{:else}<Pause size={14} aria-hidden="true" />{/if}
							Pause
						</button>
					{/if}
					<button
						type="button"
						disabled={busy}
						onclick={() => { deleteFiles = false; confirmRemove = true; }}
						class="ml-auto inline-flex h-9 items-center gap-1.5 rounded-md bg-status-failed/15 px-3.5 text-sm font-semibold text-status-failed transition hover:bg-status-failed/25 disabled:opacity-50"
					>
						<Trash2 size={14} aria-hidden="true" />
						Remove
					</button>
				</footer>
			{:else}
				<footer class="shrink-0 border-t border-border p-4 text-center text-xs text-fg-subtle">
					You have read-only access to torrents.
				</footer>
			{/if}
		</div>
	</div>
{/if}

{#snippet stat(label: string, value: string)}
	<div class="bg-bg-elevated px-3 py-2.5">
		<div class="font-mono tabular-nums text-sm font-semibold text-fg">{value}</div>
		<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.1em] text-fg-faint">
			{label}
		</div>
	</div>
{/snippet}

<Dialog
	open={confirmRemove}
	title="Remove torrent?"
	onClose={() => (confirmRemove = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Remove",
			variant: "danger",
			onClick: () => torrent && onRemove(torrent.hash, deleteFiles),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		Removes
		<span class="font-medium text-fg">{torrent?.name || "this torrent"}</span>
		from the built-in engine.
	</p>
	<Checkbox
		checked={deleteFiles}
		onChange={(v) => (deleteFiles = v)}
		tone="danger"
		class="mt-4 rounded-md border border-border bg-bg-card p-3"
	>
		<span class="text-sm text-fg-muted">
			<span class="font-medium text-fg">Also delete downloaded files</span>
			from disk. This can’t be undone.
		</span>
	</Checkbox>
</Dialog>
