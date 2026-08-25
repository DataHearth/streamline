<script lang="ts">
	import { onMount } from "svelte";
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
	import ProgressRing from "./ProgressRing.svelte";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import TorrentFilesTab from "./TorrentFilesTab.svelte";
	import TorrentPeersTab from "./TorrentPeersTab.svelte";
	import TorrentTrackersTab from "./TorrentTrackersTab.svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import {
		formatBytes,
		formatSpeed,
		formatEta,
		formatRatio,
	} from "../../lib/format";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import { m as i18n } from "../../lib/paraglide/messages.js";
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

	// Below md the same detail is a bottom sheet rather than a side drawer: it
	// arrives from the edge the thumb is on, and it's dismissed by dragging rather
	// than by finding a close target in a far corner. Same content either way — only
	// the container and the axis it travels on change, and the axis can't be a CSS
	// class, so the breakpoint is read here.
	let compact = $state(false);
	onMount(() => {
		const mql = window.matchMedia("(max-width: 767px)");
		const sync = () => (compact = mql.matches);
		sync();
		mql.addEventListener("change", sync);
		return () => mql.removeEventListener("change", sync);
	});

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
		if (!open) return;
		lockScroll();
		return unlockScroll;
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
		torrent !== null && (torrent.status === "downloading" || torrent.status === "seeding" || torrent.status === "fetching"),
	);

	const TABS: { key: Tab; label: string }[] = [
		{ key: "files", label: i18n.common_files() },
		{ key: "peers", label: i18n.torrent_peers() },
		{ key: "trackers", label: i18n.torrent_trackers() },
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
			use:sheetSwipe={{ onDismiss: onClose, disabled: !compact }}
			transition:fly={compact
				? { y: 420, duration: 280, easing: cubicOut }
				: { x: 540, duration: 240, easing: cubicOut }}
			class={cn(
				"absolute flex flex-col bg-bg-elevated shadow-4",
				compact
					? "inset-x-0 bottom-0 max-h-[88dvh] overflow-hidden rounded-t-2xl border-t border-border-strong"
					: "inset-y-0 right-0 w-full max-w-[560px] border-l border-border lg:max-w-[720px]",
			)}
			role="dialog"
			aria-modal="true"
			aria-label={i18n.torrent_detail()}
		>
			<!-- Header -->
			<header class="relative shrink-0 border-b border-border p-5">
				{#if compact}
					<span
						aria-hidden="true"
						class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
					></span>
				{/if}
				<div class="flex items-start justify-between gap-3">
					<div class="flex min-w-0 items-start gap-3">
						<ProgressRing
							status={torrent.status}
							progress={torrent.progress}
							size="lg"
						/>
						<div class="min-w-0">
							<h2 class="break-words text-base font-semibold leading-snug text-fg">
								{#if fetching && !torrent.name}
									<span class="italic text-fg-muted">{i18n.torrent_fetching_metadata()}</span>
								{:else}
									{torrent.name}
								{/if}
							</h2>
							<!-- infohash · click to copy -->
							<button
								type="button"
								onclick={copyHash}
								class="group mt-1 inline-flex max-w-full items-center gap-1.5 text-left"
								title={i18n.action_copy_infohash()}
							>
								<span
									class="truncate font-mono text-[11px] text-fg-subtle group-hover:text-fg-muted"
								>
									{torrent.hash}
								</span>
								{#if copied}
									<Check
										size={12}
										class="shrink-0 text-status-available"
										aria-hidden="true"
									/>
								{:else}
									<Copy
										size={12}
										class="shrink-0 text-fg-faint group-hover:text-fg-muted"
										aria-hidden="true"
									/>
								{/if}
							</button>
						</div>
					</div>
					<button
						type="button"
						onclick={onClose}
						aria-label={i18n.common_close()}
						class="grid h-9 w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						<X size={16} aria-hidden="true" />
					</button>
				</div>

				<div class="mt-3 flex flex-wrap items-center gap-2">
					<StatusPill status={torrent.status} live={active} />
					{#if torrent.seeding_stopped}
						<span
							class="inline-flex items-center gap-1 rounded-full border border-status-completed/40 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-completed"
							title={i18n.torrent_ratio_limit_reached()}
						>
							{i18n.torrent_seeding_stopped()}
						</span>
					{/if}
					{#if !torrent.tracked}
						<span
							class="inline-flex items-center rounded-full border border-border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-subtle"
						>
							untracked
						</span>
					{/if}
					<span class="ml-auto font-mono text-xs tabular-nums text-fg-faint">
						{#if torrent.status === "downloading" && torrent.eta > 0}
							{formatEta(torrent.eta)} left
						{:else if torrent.status === "seeding"}
							seeding
						{:else if torrent.status === "completed"}
							complete
						{/if}
					</span>
				</div>

				<!-- stat tiles. Four across at every width: TouchStatLine already proves
				     four numbers read at 390px, and a 2×2 grid split the pair apart. -->
				<div
					class="mt-4 grid grid-cols-4 gap-px overflow-hidden rounded-md border border-border bg-border"
				>
					{@render stat("Ratio", fetching ? "—" : formatRatio(torrent.ratio))}
					{@render stat("Size", formatBytes(torrent.size))}
					{@render stat("↓ Down", formatSpeed(torrent.download_speed) || "—")}
					{@render stat("↑ Up", formatSpeed(torrent.upload_speed) || "—")}
				</div>

				<!-- meta -->
				<dl class="mt-4 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-xs">
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">{i18n.torrent_save_path()}</dt>
					<dd class="min-w-0 break-all font-mono text-fg-muted">{torrent.save_path}</dd>
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">{i18n.torrent_swarm()}</dt>
					<dd class="font-mono tabular-nums text-fg-muted">
						{torrent.seeds} seeds · {torrent.peer_count} peers
					</dd>
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">{i18n.common_added()}</dt>
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
			<div data-sheet-scroll class="min-h-0 flex-1 overflow-y-auto p-4">
				{#if !detail}
					<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
						<LoaderCircle size={22} class="text-fg-faint motion-safe:animate-spin" aria-hidden="true" />
						<p class="text-sm text-fg-muted">{i18n.common_loading_details()}</p>
					</div>
				{:else if tab === "files"}
					{#if detail.files.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
							<FileText size={24} class="text-fg-faint" aria-hidden="true" />
							<p class="text-sm font-medium text-fg">{i18n.torrent_waiting_metadata()}</p>
							<p class="text-xs text-fg-muted">
								{i18n.torrent_file_list_after_magnet()}
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
				<footer
					class="flex shrink-0 items-center gap-2 border-t border-border p-4 pb-[max(env(safe-area-inset-bottom),1rem)] md:pb-4"
				>
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
						{i18n.common_remove()}
					</button>
				</footer>
			{:else}
				<footer class="shrink-0 border-t border-border p-4 text-center text-xs text-fg-subtle">
					{i18n.torrent_readonly()}
				</footer>
			{/if}
		</div>
	</div>
{/if}

{#snippet stat(label: string, value: string)}
	<div class="min-w-0 bg-bg-elevated px-2.5 py-2.5 md:px-3">
		<div
			class="truncate font-mono text-[13px] font-semibold tabular-nums text-fg md:text-sm"
		>
			{value}
		</div>
		<div
			class="mt-0.5 truncate text-[9px] font-medium uppercase tracking-[0.1em] text-fg-faint md:text-[10px]"
		>
			{label}
		</div>
	</div>
{/snippet}

<Dialog
	open={confirmRemove}
	title={i18n.torrent_remove_confirm()}
	inlineActions
	onClose={() => (confirmRemove = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_remove(),
			variant: "danger",
			onClick: () => torrent && onRemove(torrent.hash, deleteFiles),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{i18n.torrent_removes()}
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
			<span class="font-medium text-fg">{i18n.torrent_also_delete_files()}</span>
			from disk. This can’t be undone.
		</span>
	</Checkbox>
</Dialog>
