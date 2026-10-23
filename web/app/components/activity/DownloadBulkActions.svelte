<script lang="ts">
	import { useQueryClient } from "@tanstack/svelte-query";
	import { Ban, Pause, Play, Trash2 } from "@lucide/svelte";
	import { api } from "@lib/api";
	import { toast } from "@lib/toast";
	import { runBulk } from "@lib/bulk";
	import { NOUN_DOWNLOAD } from "@lib/nouns";
	import BulkActionBar from "@components/shared/BulkActionBar.svelte";
	import BulkTouchBar from "@components/shared/BulkTouchBar.svelte";
	import type {
		TouchAction,
		TouchMenuRow,
	} from "@components/shared/BulkTouchBar.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import type { HistoryEntry, QueueEntry } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Entry = QueueEntry | HistoryEntry;

	let {
		view,
		items,
		selected,
		onSelectAll,
		onClear,
	}: {
		view: "queue" | "history";
		// The rows the table shows, so "select all" follows the active filters.
		items: Entry[];
		selected: Set<number>;
		onSelectAll: () => void;
		onClear: () => void;
	} = $props();

	let picked = $derived(items.filter((i) => selected.has(i.id)));
	let count = $derived(picked.length);
	// The rules the expanded row already follows. A held record is off the
	// network and the server refuses to pause or cancel it — resolving is the
	// only move — so it is left out of every queue action rather than counted as
	// a failure. Each action runs on its own subset, so the toast's count is
	// what was acted on.
	let live = $derived(picked.filter((i) => i.status !== "held"));
	let pausable = $derived(live.filter((i) => i.status !== "paused"));
	let resumable = $derived(live.filter((i) => i.status === "paused"));
	let heldPicked = $derived(count - live.length);
	let confirmOpen = $state(false);
	let busy = $state(false);

	const qc = useQueryClient();

	type Done = (inputs: { items: string }) => string;

	function report(
		done: Done,
		res: { ok: number; failed: number; firstError?: string },
	) {
		if (res.failed === 0) toast.ok(done({ items: NOUN_DOWNLOAD.count(res.ok) }));
		else if (res.ok === 0) toast.err(res.firstError ?? i18n.bulk_failed_all());
		else toast.err(i18n.bulk_partial({ ok: res.ok, failed: res.failed }));
	}

	async function run(
		done: Done,
		subset: Entry[],
		fn: (i: Entry) => Promise<unknown>,
		after?: () => void,
	) {
		if (busy || subset.length === 0) return;
		busy = true;
		try {
			const res = await runBulk(subset, fn);
			qc.invalidateQueries({ queryKey: ["activity", "queue"] });
			qc.invalidateQueries({ queryKey: ["activity", "history"] });
			// Cancelling removes the torrent from its client too.
			qc.invalidateQueries({ queryKey: ["activity", "torrents"] });
			report(done, res);
			after?.();
			if (res.failed === 0) onClear();
		} finally {
			busy = false;
		}
	}

	function pause() {
		run(i18n.bulk_paused, pausable, (i) =>
			api(`/activity/queue/${i.id}/pause`, { method: "POST" }),
		);
	}
	function resume() {
		run(i18n.bulk_resumed, resumable, (i) =>
			api(`/activity/queue/${i.id}/resume`, { method: "POST" }),
		);
	}
	function confirm() {
		if (view === "queue")
			run(
				i18n.bulk_cancelled,
				live,
				(i) => api(`/activity/queue/${i.id}`, { method: "DELETE" }),
				() => (confirmOpen = false),
			);
		else
			run(
				i18n.bulk_removed,
				picked,
				(i) => api(`/activity/history/${i.id}`, { method: "DELETE" }),
				() => (confirmOpen = false),
			);
	}

	let confirmCount = $derived(view === "queue" ? live.length : count);

	const btn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";
	const dangerBtn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-status-failed/40 bg-status-failed/10 px-3 text-[12.5px] font-medium text-status-failed transition hover:bg-status-failed/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

	let heldLine = $derived(heldPicked > 0 ? i18n.bulk_held_skipped() : undefined);

	let touchActions = $derived<TouchAction[]>(
		view === "queue"
			? [
					{
						key: "pause",
						label: i18n.common_pause(),
						icon: Pause,
						disabled: pausable.length === 0,
						onSelect: pause,
					},
					{
						key: "resume",
						label: i18n.common_resume(),
						icon: Play,
						disabled: resumable.length === 0,
						onSelect: resume,
					},
					{
						key: "cancel",
						label: i18n.common_cancel(),
						icon: Ban,
						danger: true,
						disabled: live.length === 0,
						onSelect: () => (confirmOpen = true),
					},
				]
			: [
					{
						key: "remove",
						label: i18n.common_remove(),
						icon: Trash2,
						danger: true,
						onSelect: () => (confirmOpen = true),
					},
				],
	);

	let touchMenu = $derived<TouchMenuRow[]>(
		view === "queue"
			? [
					{
						key: "pause",
						label: i18n.common_pause(),
						icon: Pause,
						disabled: pausable.length === 0,
						line:
							pausable.length === 0
								? i18n.bulk_none_pausable()
								: (heldLine ??
									i18n.bulk_already_paused({
										done: resumable.length,
										total: live.length,
									})),
						onSelect: pause,
					},
					{
						key: "resume",
						label: i18n.common_resume(),
						icon: Play,
						disabled: resumable.length === 0,
						line: resumable.length === 0 ? i18n.bulk_none_paused() : heldLine,
						onSelect: resume,
					},
					{
						key: "cancel",
						label: i18n.action_cancel_download(),
						icon: Ban,
						danger: true,
						dividerBefore: true,
						disabled: live.length === 0,
						line: heldLine,
						onSelect: () => (confirmOpen = true),
					},
				]
			: [
					{
						key: "remove",
						label: i18n.common_remove(),
						icon: Trash2,
						danger: true,
						onSelect: () => (confirmOpen = true),
					},
				],
	);
</script>

{#if count > 0}
	<div class="hidden md:block">
		<BulkActionBar
			{count}
			total={items.length}
			noun={NOUN_DOWNLOAD}
			{busy}
			{onSelectAll}
			{onClear}
		>
			{#if view === "queue"}
				<button
					type="button"
					disabled={busy || pausable.length === 0}
					title={pausable.length === 0 ? i18n.bulk_none_pausable() : heldLine}
					onclick={pause}
					class={btn}
				>
					<Pause size={14} aria-hidden="true" />
					{i18n.common_pause()}
				</button>
				<button
					type="button"
					disabled={busy || resumable.length === 0}
					title={resumable.length === 0 ? i18n.bulk_none_paused() : heldLine}
					onclick={resume}
					class={btn}
				>
					<Play size={14} aria-hidden="true" />
					{i18n.common_resume()}
				</button>
				<button
					type="button"
					disabled={busy || live.length === 0}
					title={heldLine}
					onclick={() => (confirmOpen = true)}
					class={dangerBtn}
				>
					<Ban size={14} aria-hidden="true" />
					{i18n.action_cancel_download()}
				</button>
			{:else}
				<button
					type="button"
					disabled={busy}
					onclick={() => (confirmOpen = true)}
					class={dangerBtn}
				>
					<Trash2 size={14} aria-hidden="true" />
					{i18n.common_remove()}
				</button>
			{/if}
		</BulkActionBar>
	</div>

	<BulkTouchBar
		{count}
		noun={NOUN_DOWNLOAD}
		{busy}
		actions={touchActions}
		menu={touchMenu}
	/>
{/if}

<Dialog
	open={confirmOpen}
	title={view === "queue"
		? i18n.bulk_cancel_title({ items: NOUN_DOWNLOAD.count(confirmCount) })
		: i18n.bulk_remove_history_title({ items: NOUN_DOWNLOAD.count(confirmCount) })}
	body={view === "queue"
		? i18n.bulk_cancel_body()
		: i18n.bulk_remove_history_body()}
	onClose={() => (confirmOpen = false)}
	actions={[
		{
			label: view === "queue" ? i18n.common_keep() : i18n.common_cancel(),
			variant: "ghost",
			autofocus: true,
		},
		{
			label: view === "queue" ? i18n.action_cancel_download() : i18n.common_remove(),
			variant: "danger",
			dismiss: false,
			pending: busy,
			onClick: confirm,
		},
	]}
/>
