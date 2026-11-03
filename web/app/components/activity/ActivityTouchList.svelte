<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		Activity,
		Ban,
		LoaderCircle,
		Pause,
		Play,
		RotateCw,
		Trash2,
	} from "@lucide/svelte";
	import TouchRow, { type RowSwipe } from "./TouchRow.svelte";
	import type { DownloadConfirm } from "./DownloadConfirmDialog.svelte";
	import { entryHeading, historyMeta, queueMeta } from "@lib/activity-touch";
	import { pillStatus } from "@lib/format";
	import type { HistoryEntry, QueueEntry } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { errorText } from "@lib/api";

	// The table's replacement below md: one surface, hairline rows, and the ring
	// carrying what the Status and Progress columns used to. Everything else the
	// table showed moves into the row's third line or the detail sheet.
	let {
		view,
		rows,
		loading,
		error,
		hasMore = false,
		loadingMore = false,
		onLoadMore,
		selected,
		selecting = false,
		onToggle,
		onBeginSelect,
		onOpen,
		onResolve,
		onPause,
		onResume,
		onConfirm,
	}: {
		view: "queue" | "history";
		rows: (QueueEntry | HistoryEntry)[];
		loading: boolean;
		error: Error | null;
		hasMore?: boolean;
		loadingMore?: boolean;
		onLoadMore: () => void;
		// Bulk selection, given only to someone who may act on it. While
		// `selecting` a tap toggles the row rather than opening its sheet.
		selected?: Set<number>;
		selecting?: boolean;
		onToggle?: (id: number) => void;
		onBeginSelect?: (id: number) => void;
		onOpen: (item: QueueEntry | HistoryEntry) => void;
		onResolve?: (item: QueueEntry) => void;
		// Swipe actions, given only to someone who may act on them. Pause and
		// resume run at once, as the sheet's buttons do; cancel, retry and
		// remove go through onConfirm to the same question the sheet asks.
		onPause?: (id: number) => void;
		onResume?: (id: number) => void;
		onConfirm?: (item: QueueEntry | HistoryEntry, kind: DownloadConfirm) => void;
	} = $props();

	// The sheet's own rules, one swipe per button it shows: a held record has
	// none (the server refuses pause and cancel on it, and Resolve is on the
	// row), and only a failed history entry can be retried.
	function swipes(row: QueueEntry | HistoryEntry): {
		start?: RowSwipe;
		end?: RowSwipe;
	} {
		if (!onConfirm) return {};
		if (view === "queue") {
			if (row.status === "held") return {};
			const paused = row.status === "paused";
			const toggle = paused ? onResume : onPause;
			return {
				start: toggle && {
					label: paused ? i18n.schedule_resume() : i18n.schedule_pause(),
					icon: paused ? Play : Pause,
					tone: paused ? "var(--status-downloading)" : "var(--status-warning)",
					onSwipe: () => toggle(row.id),
				},
				end: {
					label: i18n.common_cancel(),
					icon: Ban,
					tone: "var(--status-failed)",
					onSwipe: () => onConfirm(row, "cancel"),
				},
			};
		}
		return {
			start:
				row.status === "failed"
					? {
							label: i18n.activity_retry_import(),
							icon: RotateCw,
							tone: "var(--accent)",
							onSwipe: () => onConfirm(row, "retry"),
						}
					: undefined,
			end: {
				label: i18n.common_remove(),
				icon: Trash2,
				tone: "var(--status-failed)",
				onSwipe: () => onConfirm(row, "remove"),
			},
		};
	}

	// Same contract as the table's sentinel: it only exists in the history view,
	// so it mounts long after this component and re-mounts on every switch.
	// Keying the effect on the binding re-attaches the observer each time.
	let sentinel = $state<HTMLDivElement | null>(null);
	$effect(() => {
		const el = sentinel;
		if (!el) return;
		const io = new IntersectionObserver((entries) => {
			if (entries[0]?.isIntersecting && hasMore && !loadingMore) onLoadMore();
		});
		io.observe(el);
		return () => io.disconnect();
	});

	// Importing has finished downloading, so its ring is full even though the
	// entry isn't done; ringReading gives it the spinning arc regardless.
	function progressOf(item: QueueEntry | HistoryEntry): number {
		if (view === "history") return 1;
		const q = item as QueueEntry;
		return q.status === "importing" ? 1 : (q.progress ?? 0);
	}
</script>

<div class="mt-3 md:hidden">
	{#if loading}
		<div class="flex flex-col gap-2.5"><SkeletonList variant="row" count={4} /></div>
	{:else if error}
		<div
			class="rounded-xl border border-status-failed/25 bg-bg-elevated px-5 py-9 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				{view === "queue"
					? i18n.activity_load_failed_queue()
					: i18n.activity_load_failed_history()}
			</p>
			<p class="mt-1 font-mono text-[11px] text-fg-subtle">
				{errorText(error)}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center gap-1.5 rounded-xl border border-border bg-bg-elevated px-5 py-11 text-center"
		>
			<Activity size={26} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-semibold text-fg">
				{view === "queue" ? i18n.activity_queue_quiet() : i18n.common_no_history()}
			</p>
			<p class="max-w-[16rem] text-xs text-fg-muted">
				{view === "queue"
					? i18n.activity_queue_help()
					: i18n.activity_none_completed()}
			</p>
		</div>
	{:else}
		<div class="overflow-hidden rounded-xl border border-border bg-bg-elevated">
			{#each rows as row (row.id)}
				{@const held =
					view === "queue" && (row as QueueEntry).status === "held"}
				{@const swipe = swipes(row)}
				<TouchRow
					status={pillStatus(row.status)}
					progress={progressOf(row)}
					title={entryHeading(row)}
					release={row.title}
					meta={view === "queue"
						? queueMeta(row as QueueEntry)
						: historyMeta(row as HistoryEntry)}
					selected={selecting ? (selected?.has(row.id) ?? false) : undefined}
					onLongPress={onBeginSelect ? () => onBeginSelect(row.id) : undefined}
					onOpen={() =>
						selecting && onToggle ? onToggle(row.id) : onOpen(row)}
					onResolve={held && onResolve
						? () => onResolve(row as QueueEntry)
						: undefined}
					resolveLabel={i18n.action_resolve()}
					swipeStart={swipe.start}
					swipeEnd={swipe.end}
				/>
			{/each}
		</div>
		{#if view === "history"}
			<div bind:this={sentinel} class="h-px w-full"></div>
			{#if loadingMore}
				<div
					class="flex items-center justify-center gap-2 py-3.5 text-xs text-fg-muted"
				>
					<LoaderCircle
						size={14}
						class="motion-safe:animate-spin"
						aria-hidden="true"
					/>
					{i18n.common_loading_more()}
				</div>
			{/if}
		{/if}
	{/if}
</div>
