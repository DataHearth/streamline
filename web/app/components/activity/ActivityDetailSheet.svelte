<script lang="ts">
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Ban, LoaderCircle, Pause, Play, Trash2, X } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";
	import ProgressRing from "./ProgressRing.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import { entryHeading, historyMeta, queueMeta } from "../../lib/activity-touch";
	import { formatBytes, pillStatus } from "../../lib/format";
	import { formatDateTime } from "../../lib/dates";
	import type { HistoryEntry, QueueEntry } from "../../lib/types";

	// Where a queue or history row opens below md, and the only place pause,
	// resume, cancel and remove exist on touch — no swipe, no per-row kebab, so
	// there's one way to reach a verb and it's labelled.
	let {
		item,
		view,
		busy = false,
		canControl = false,
		onClose,
		onCancel,
		onPause,
		onResume,
		onRemove,
	}: {
		item: QueueEntry | HistoryEntry | null;
		view: "queue" | "history";
		busy?: boolean;
		canControl?: boolean;
		onClose: () => void;
		onCancel: (id: number) => void;
		onPause: (id: number) => void;
		onResume: (id: number) => void;
		onRemove: (id: number) => void;
	} = $props();

	let confirmCancel = $state(false);
	let confirmRemove = $state(false);

	$effect(() => {
		if (!item) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

	let queue = $derived(item as QueueEntry | null);
	let history = $derived(item as HistoryEntry | null);
	let isPaused = $derived(view === "queue" && item?.status === "paused");
	let progress = $derived(
		view === "history" ? 1 : queue?.status === "importing" ? 1 : (queue?.progress ?? 0),
	);
	let meta = $derived(
		item
			? view === "queue"
				? queueMeta(item as QueueEntry)
				: historyMeta(item as HistoryEntry)
			: null,
	);

	type KV = { label: string; value: string; tone?: string };
	let rows = $derived.by<KV[]>(() => {
		if (!item) return [];
		const out: KV[] = [
			{ label: "Release", value: item.title },
			{ label: "Indexer", value: item.indexer || "—" },
			{ label: "Created", value: formatDateTime(item.created_at) },
		];
		if (view === "history") {
			out.push({
				label: "Imported",
				value: history?.imported_at ? formatDateTime(history.imported_at) : "—",
			});
		}
		if (item.failure_reason) {
			out.push({
				label: "Error",
				value: item.failure_reason,
				tone: "var(--status-failed)",
			});
		}
		return out;
	});
</script>

{#if item}
	<div
		class="fixed inset-0 z-50 md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label="Download detail"
	>
		<button
			type="button"
			aria-label="Close"
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-start justify-between gap-3 px-5 pb-3 pt-5 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<div class="flex min-w-0 items-center gap-3">
					<ProgressRing status={pillStatus(item.status)} {progress} />
					<div class="min-w-0">
						<h2 class="truncate text-[16.5px] font-semibold tracking-tight text-fg">
							{entryHeading(item)}
						</h2>
						<p class="truncate font-mono text-[11px] text-fg-subtle">{item.title}</p>
					</div>
				</div>
				<button
					type="button"
					onclick={onClose}
					aria-label="Close"
					class="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-3"
			>
				<div class="flex items-center justify-between gap-3">
					<StatusPill status={pillStatus(item.status)} live={item.status === "downloading"} />
					<span
						class="truncate font-mono text-[11.5px]"
						style:color={meta?.color ?? "var(--fg-muted)"}
					>
						{meta?.text}
					</span>
				</div>

				<div
					class="mt-3.5 grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border"
				>
					<div class="bg-bg-elevated px-3 py-2.5">
						<div class="font-mono text-[13.5px] font-semibold tabular-nums text-fg">
							{formatBytes(item.size)}
						</div>
						<div
							class="mt-px text-[9px] font-medium uppercase tracking-[0.12em] text-fg-faint"
						>
							Size
						</div>
					</div>
					<div class="bg-bg-elevated px-3 py-2.5">
						<div class="truncate font-mono text-[12.5px] font-semibold text-fg">
							{item.download_client || "—"}
						</div>
						<div
							class="mt-px text-[9px] font-medium uppercase tracking-[0.12em] text-fg-faint"
						>
							Client
						</div>
					</div>
				</div>

				<dl class="mt-3.5 grid grid-cols-[max-content_1fr] gap-x-3.5 gap-y-1.5">
					{#each rows as kv (kv.label)}
						<dt
							class="pt-px text-[9.5px] font-medium uppercase tracking-[0.1em]"
							style:color={kv.tone ?? "var(--fg-faint)"}
						>
							{kv.label}
						</dt>
						<dd
							class="min-w-0 break-words font-mono text-[11.5px]"
							style:color={kv.tone ?? "var(--fg-muted)"}
						>
							{kv.value}
						</dd>
					{/each}
				</dl>
			</div>

			{#if canControl}
				<div
					class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
				>
					{#if view === "queue"}
						<button
							type="button"
							disabled={busy}
							onclick={() => (isPaused ? onResume(item.id) : onPause(item.id))}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-semibold text-fg transition active:bg-surface-2 disabled:opacity-50"
						>
							{#if busy}
								<LoaderCircle size={16} class="motion-safe:animate-spin" aria-hidden="true" />
							{:else if isPaused}
								<Play size={16} aria-hidden="true" />
							{:else}
								<Pause size={16} aria-hidden="true" />
							{/if}
							{isPaused ? "Resume" : "Pause"}
						</button>
						<button
							type="button"
							disabled={busy}
							onclick={() => (confirmCancel = true)}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl bg-status-failed/15 text-[14px] font-semibold text-status-failed transition active:bg-status-failed/25 disabled:opacity-50"
						>
							<Ban size={16} aria-hidden="true" />
							Cancel
						</button>
					{:else}
						<button
							type="button"
							disabled={busy}
							onclick={() => (confirmRemove = true)}
							class={cn(
								"inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl bg-status-failed/15 text-[14px] font-semibold text-status-failed transition active:bg-status-failed/25 disabled:opacity-50",
							)}
						>
							<Trash2 size={16} aria-hidden="true" />
							Remove from history
						</button>
					{/if}
				</div>
			{:else}
				<div
					class="border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5 text-center text-xs text-fg-subtle"
				>
					You have read-only access to downloads.
				</div>
			{/if}
		</div>
	</div>

	<Dialog
		open={confirmCancel}
		title="Cancel download?"
		inlineActions
		onClose={() => (confirmCancel = false)}
		actions={[
			{ label: "Keep", variant: "ghost", autofocus: true },
			{
				label: "Cancel download",
				variant: "danger",
				onClick: () => {
					onCancel(item.id);
					onClose();
				},
			},
		]}
	>
		<p class="text-sm text-fg-muted">
			Removes the torrent and its partial files from the client, then deletes
			<span class="font-medium text-fg">{item.title}</span> from the queue. The movie
			returns to <em>wanted</em> if it has no file yet.
		</p>
	</Dialog>

	<Dialog
		open={confirmRemove}
		title="Remove history record?"
		inlineActions
		onClose={() => (confirmRemove = false)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label: "Remove",
				variant: "danger",
				onClick: () => {
					onRemove(item.id);
					onClose();
				},
			},
		]}
	>
		<p class="text-sm text-fg-muted">
			Deletes the history entry for
			<span class="font-medium text-fg">{item.title}</span>. The movie and its files
			are not affected.
		</p>
	</Dialog>
{/if}
