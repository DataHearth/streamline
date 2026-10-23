<script lang="ts">
	import { useQueryClient } from "@tanstack/svelte-query";
	import { Pause, Play, Trash2 } from "@lucide/svelte";
	import { api } from "@lib/api";
	import { toast } from "@lib/toast";
	import { runBulk } from "@lib/bulk";
	import { NOUN_TORRENT } from "@lib/nouns";
	import BulkActionBar from "@components/shared/BulkActionBar.svelte";
	import BulkTouchBar from "@components/shared/BulkTouchBar.svelte";
	import type {
		TouchAction,
		TouchMenuRow,
	} from "@components/shared/BulkTouchBar.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import type { Torrent } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		torrents,
		selected,
		onSelectAll,
		onClear,
	}: {
		// The rows the table shows, so "select all" follows the active filters.
		torrents: Torrent[];
		selected: Set<string>;
		onSelectAll: () => void;
		onClear: () => void;
	} = $props();

	let picked = $derived(torrents.filter((t) => selected.has(t.hash)));
	let count = $derived(picked.length);
	// The same split the drawer makes: a paused torrent resumes, anything moving
	// pauses, and a completed one has nothing to pause. Each action runs on its
	// own subset so the toast's count is what was actually acted on.
	let pausable = $derived(
		picked.filter(
			(t) =>
				t.status === "downloading" ||
				t.status === "seeding" ||
				t.status === "fetching" ||
				t.status === "stalled",
		),
	);
	let resumable = $derived(picked.filter((t) => t.status === "paused"));
	let removeOpen = $state(false);
	let busy = $state(false);

	const qc = useQueryClient();

	type Done = (inputs: { items: string }) => string;

	function report(
		done: Done,
		res: { ok: number; failed: number; firstError?: string },
	) {
		if (res.failed === 0) toast.ok(done({ items: NOUN_TORRENT.count(res.ok) }));
		else if (res.ok === 0) toast.err(res.firstError ?? i18n.bulk_failed_all());
		else toast.err(i18n.bulk_partial({ ok: res.ok, failed: res.failed }));
	}

	async function run(
		done: Done,
		items: Torrent[],
		fn: (t: Torrent) => Promise<unknown>,
		after?: () => void,
	) {
		if (busy || items.length === 0) return;
		busy = true;
		try {
			const res = await runBulk(items, fn);
			qc.invalidateQueries({ queryKey: ["activity", "torrents"] });
			report(done, res);
			after?.();
			if (res.failed === 0) onClear();
		} finally {
			busy = false;
		}
	}

	function pause() {
		run(i18n.bulk_paused, pausable, (t) =>
			api(`/torrents/${t.hash}/pause`, { method: "POST" }),
		);
	}
	function resume() {
		run(i18n.bulk_resumed, resumable, (t) =>
			api(`/torrents/${t.hash}/resume`, { method: "POST" }),
		);
	}
	function remove(withFiles: boolean) {
		run(
			i18n.bulk_removed,
			picked,
			(t) =>
				api(`/torrents/${t.hash}?delete_files=${withFiles}`, {
					method: "DELETE",
				}),
			() => {
				// The server purges the download record behind each torrent, so the
				// Activity queue is stale the moment this returns.
				qc.invalidateQueries({ queryKey: ["activity", "queue"] });
				removeOpen = false;
			},
		);
	}

	const btn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";
	const dangerBtn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-status-failed/40 bg-status-failed/10 px-3 text-[12.5px] font-medium text-status-failed transition hover:bg-status-failed/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

	let touchActions = $derived<TouchAction[]>([
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
			key: "remove",
			label: i18n.common_remove(),
			icon: Trash2,
			danger: true,
			onSelect: () => (removeOpen = true),
		},
	]);

	let touchMenu = $derived<TouchMenuRow[]>([
		{
			key: "pause",
			label: i18n.common_pause(),
			icon: Pause,
			disabled: pausable.length === 0,
			line:
				pausable.length === 0
					? i18n.bulk_none_pausable()
					: i18n.bulk_already_paused({
							done: resumable.length,
							total: count,
						}),
			onSelect: pause,
		},
		{
			key: "resume",
			label: i18n.common_resume(),
			icon: Play,
			disabled: resumable.length === 0,
			line: resumable.length === 0 ? i18n.bulk_none_paused() : undefined,
			onSelect: resume,
		},
		{
			key: "remove",
			label: i18n.common_remove(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => (removeOpen = true),
		},
	]);
</script>

{#if count > 0}
	<div class="hidden md:block">
		<BulkActionBar
			{count}
			total={torrents.length}
			noun={NOUN_TORRENT}
			{busy}
			{onSelectAll}
			{onClear}
		>
			<button
				type="button"
				disabled={busy || pausable.length === 0}
				title={pausable.length === 0 ? i18n.bulk_none_pausable() : undefined}
				onclick={pause}
				class={btn}
			>
				<Pause size={14} aria-hidden="true" />
				{i18n.common_pause()}
			</button>
			<button
				type="button"
				disabled={busy || resumable.length === 0}
				title={resumable.length === 0 ? i18n.bulk_none_paused() : undefined}
				onclick={resume}
				class={btn}
			>
				<Play size={14} aria-hidden="true" />
				{i18n.common_resume()}
			</button>
			<button
				type="button"
				disabled={busy}
				onclick={() => (removeOpen = true)}
				class={dangerBtn}
			>
				<Trash2 size={14} aria-hidden="true" />
				{i18n.common_remove()}
			</button>
		</BulkActionBar>
	</div>

	<BulkTouchBar
		{count}
		noun={NOUN_TORRENT}
		{busy}
		actions={touchActions}
		menu={touchMenu}
	/>
{/if}

<DeleteTitleDialog
	open={removeOpen}
	title={i18n.bulk_remove_torrents_title({ items: NOUN_TORRENT.count(count) })}
	body={i18n.bulk_remove_torrents_body()}
	filesLabel={i18n.torrent_also_delete_files()}
	pending={busy}
	onClose={() => (removeOpen = false)}
	onConfirm={remove}
/>
