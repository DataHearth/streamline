<script lang="ts" module>
	// One selectable title on the music or books library: an artist, a book or
	// a book series, addressed by its REST path like LibraryActions.
	export type BulkRow = {
		key: string;
		base: string;
		hasFiles: boolean;
		// The PATCH `monitor` values the bar's two buttons write, and whether the
		// title is followed now. Absent where one switch has no single meaning: a
		// book follows two formats.
		monitor?: { on: string; off: string; current: boolean };
	};
</script>

<script lang="ts">
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { Bookmark, BookmarkX, FileEdit, Radar, RefreshCw, SlidersHorizontal, Trash2 } from "@lucide/svelte";
	import { api } from "@lib/api";
	import { toast } from "@lib/toast";
	import { runBulk } from "@lib/bulk";
	import type { Noun } from "@lib/nouns";
	import { profilesPath } from "@lib/music-books";
	import type { QualityProfile } from "@lib/types";
	import BulkActionBar from "./BulkActionBar.svelte";
	import BulkTouchBar, { type TouchAction, type TouchMenuRow } from "./BulkTouchBar.svelte";
	import KebabMenu, { type KebabItem } from "./KebabMenu.svelte";
	import QualityProfileModal from "./QualityProfileModal.svelte";
	import DeleteTitleDialog from "./DeleteTitleDialog.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The music and books twin of {Movie,Series}BulkActions: the same bar, the
	// same promoted buttons, each action one request per selected title.
	let {
		rows,
		selected,
		total,
		noun,
		media,
		removeBody,
		filesLabel,
		onSelectAll,
		onClear,
	}: {
		// Every selectable title under the active filters: the whole match after
		// "select all", the loaded cards otherwise.
		rows: BulkRow[];
		selected: Set<string>;
		total: number;
		noun: Noun;
		media: "music" | "books";
		removeBody: string;
		filesLabel: string;
		onSelectAll: () => void;
		onClear: () => void;
	} = $props();

	let count = $derived(selected.size);
	let picked = $derived(rows.filter((r) => selected.has(r.key)));
	let withFiles = $derived(picked.filter((r) => r.hasFiles));
	// Monitor only touches what is not followed, so an artist on a finer policy
	// (future, manual) keeps it.
	let canMonitor = $derived(picked.some((r) => r.monitor));
	let toMonitor = $derived(picked.filter((r) => r.monitor && !r.monitor.current));
	let toUnmonitor = $derived(picked.filter((r) => r.monitor?.current));
	let qpOpen = $state(false);
	let deleteOpen = $state(false);
	let renameOpen = $state(false);
	let busy = $state(false);

	const qc = useQueryClient();
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles", media],
		queryFn: () => api<QualityProfile[]>(profilesPath(media)),
		enabled: qpOpen,
	}));

	type Done = (inputs: { items: string }) => string;

	// The toast counts the targets, the subset an action applies to.
	async function run(done: Done, fn: (r: BulkRow) => Promise<unknown>, targets: BulkRow[] = picked, after?: () => void) {
		if (busy) return;
		busy = true;
		try {
			const res = await runBulk(targets, fn);
			qc.invalidateQueries({ queryKey: [media] });
			if (res.failed === 0) toast.ok(done({ items: noun.count(res.ok) }));
			else if (res.ok === 0) toast.err(res.firstError ?? i18n.bulk_failed_all());
			else toast.err(i18n.bulk_partial({ ok: res.ok, failed: res.failed }));
			after?.();
			if (res.failed === 0) onClear();
		} finally {
			busy = false;
		}
	}

	const patch = (r: BulkRow, body: Record<string, unknown>) => api(r.base, { method: "PATCH", body });
	const post = (r: BulkRow, op: string) => api(`${r.base}/${op}`, { method: "POST" });

	const monitor = () => run(i18n.bulk_monitoring, (r) => patch(r, { monitor: r.monitor?.on }), toMonitor);
	const unmonitor = () => run(i18n.bulk_unmonitoring, (r) => patch(r, { monitor: r.monitor?.off }), toUnmonitor);
	const searchNow = () => run(i18n.bulk_search_dispatched, (r) => post(r, "search-now"));
	const refresh = () => run(i18n.bulk_refresh_requested, (r) => post(r, "refresh-metadata"));
	const renameFiles = () => run(i18n.bulk_renamed, (r) => post(r, "rename"), withFiles, () => (renameOpen = false));
	const saveProfile = (profile: string) =>
		run(i18n.bulk_reprofiled, (r) => patch(r, { quality_profile: profile }), picked, () => (qpOpen = false));
	const remove = (files: boolean) =>
		run(i18n.bulk_deleted, (r) => api(`${r.base}?delete_files=${files}`, { method: "DELETE" }), picked, () => (deleteOpen = false));

	let menuItems = $derived<KebabItem[]>([
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: withFiles.length === 0,
			title: withFiles.length === 0 ? i18n.library_available_after_import() : undefined,
			onSelect: () => (renameOpen = true),
		},
		{ key: "refresh", label: i18n.action_refresh_metadata(), icon: RefreshCw, onSelect: refresh },
		{
			key: "delete",
			label: i18n.action_remove_from_library(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => (deleteOpen = true),
		},
	]);

	const btn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

	let monitorRows = $derived<TouchMenuRow[]>(
		canMonitor
			? [
					{
						key: "monitor",
						label: i18n.action_monitor(),
						icon: Bookmark,
						disabled: toMonitor.length === 0,
						line: i18n.bulk_already_monitored({ done: toUnmonitor.length, total: count }),
						onSelect: monitor,
					},
					{
						key: "unmonitor",
						label: i18n.action_stop_monitoring(),
						icon: BookmarkX,
						disabled: toUnmonitor.length === 0,
						onSelect: unmonitor,
					},
				]
			: [],
	);
	let touchActions = $derived<TouchAction[]>([
		...monitorRows.slice(0, 1),
		{ key: "search", label: i18n.common_search(), icon: Radar, onSelect: searchNow },
	]);
	let touchMenu = $derived<TouchMenuRow[]>([
		...monitorRows,
		{ key: "search", label: i18n.common_search(), icon: Radar, line: i18n.bulk_search_per_title(), onSelect: searchNow },
		{ key: "quality", label: i18n.action_change_quality_profile(), icon: SlidersHorizontal, onSelect: () => (qpOpen = true) },
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: withFiles.length === 0,
			line: withFiles.length === 0 ? i18n.bulk_no_files() : i18n.bulk_with_files({ items: noun.count(withFiles.length) }),
			onSelect: () => (renameOpen = true),
		},
		{ key: "refresh", label: i18n.action_refresh_metadata(), icon: RefreshCw, onSelect: refresh },
		{
			key: "delete",
			label: i18n.action_remove_from_library(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			line: withFiles.length === 0 ? i18n.bulk_no_files() : i18n.bulk_with_files({ items: noun.count(withFiles.length) }),
			onSelect: () => (deleteOpen = true),
		},
	]);
</script>

{#if count > 0}
	<div class="hidden md:block">
		<BulkActionBar {count} {total} {busy} {noun} {onSelectAll} {onClear}>
			{#if canMonitor}
				<button type="button" disabled={busy || toMonitor.length === 0} onclick={monitor} class={btn}>
					<Bookmark size={14} aria-hidden="true" />
					{i18n.action_monitor()}
				</button>
				<button type="button" disabled={busy || toUnmonitor.length === 0} onclick={unmonitor} class={btn}>
					<BookmarkX size={14} aria-hidden="true" />
					{i18n.action_unmonitor()}
				</button>
			{/if}
			<button type="button" disabled={busy} onclick={searchNow} class={btn}>
				<Radar size={14} aria-hidden="true" />
				{i18n.common_search()}
			</button>
			<button type="button" disabled={busy} onclick={() => (qpOpen = true)} class={btn}>
				<SlidersHorizontal size={14} aria-hidden="true" />
				{i18n.common_quality()}
			</button>
			<KebabMenu items={menuItems} variant="bar" />
		</BulkActionBar>
	</div>

	<BulkTouchBar {count} {busy} {noun} actions={touchActions} menu={touchMenu} />
{/if}

<QualityProfileModal
	open={qpOpen}
	profiles={profilesQuery.data ?? []}
	saving={busy}
	onClose={() => (qpOpen = false)}
	onSave={saveProfile}
/>
<Dialog
	open={renameOpen}
	title={i18n.bulk_rename_title({ items: noun.count(withFiles.length) })}
	body={i18n.bulk_rename_library_body()}
	onClose={() => (renameOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: busy ? i18n.common_applying() : i18n.action_rename_files(),
			variant: "primary",
			dismiss: false,
			pending: busy,
			onClick: renameFiles,
		},
	]}
/>
<DeleteTitleDialog
	open={deleteOpen}
	title={i18n.bulk_remove_title({ items: noun.count(count) })}
	body={removeBody}
	{filesLabel}
	filesNote={i18n.common_cannot_undo()}
	canDeleteFiles={withFiles.length > 0}
	pending={busy}
	onClose={() => (deleteOpen = false)}
	onConfirm={remove}
/>
