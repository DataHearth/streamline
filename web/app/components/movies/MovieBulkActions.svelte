<script lang="ts">
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import {
		Bookmark,
		BookmarkX,
		Radar,
		SlidersHorizontal,
		RefreshCw,
		Trash2,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { runBulk, plural } from "../../lib/bulk";
	import { formatBytes } from "../../lib/format";
	import BulkActionBar from "../shared/BulkActionBar.svelte";
	import BulkTouchBar from "../shared/BulkTouchBar.svelte";
	import type {
		TouchAction,
		TouchMenuRow,
	} from "../shared/BulkTouchBar.svelte";
	import KebabMenu from "../shared/KebabMenu.svelte";
	import type { KebabItem } from "../shared/KebabMenu.svelte";
	import QualityProfileModal from "./QualityProfileModal.svelte";
	import DeleteTitleDialog from "../shared/DeleteTitleDialog.svelte";
	import type { Movie, QualityProfile } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		movies,
		selected,
		total,
		onSelectAll,
		onClear,
	}: {
		// The currently visible movies, so "select all" and the file tallies
		// follow the active filters rather than the whole library.
		movies: Movie[];
		selected: Set<number>;
		total: number;
		onSelectAll: () => void;
		onClear: () => void;
	} = $props();

	let count = $derived(selected.size);
	let active = $derived(count > 0);
	let picked = $derived(movies.filter((m) => selected.has(m.id)));
	// Off the list response's file rollup; media_files is detail-only.
	let fileCount = $derived(
		picked.reduce((n, m) => n + (m.file_summary?.file_count ?? 0), 0),
	);
	let pickedBytes = $derived(
		picked.reduce((n, m) => n + (m.file_summary?.size_bytes ?? 0), 0),
	);
	let monitoredPicked = $derived(picked.filter((m) => m.monitored).length);
	let qpOpen = $state(false);
	let deleteOpen = $state(false);
	let busy = $state(false);

	const qc = useQueryClient();

	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: qpOpen,
	}));

	function report(
		verb: string,
		res: { ok: number; failed: number; firstError?: string },
	) {
		if (res.failed === 0) toast.ok(`${verb} ${plural(res.ok, "title")}`);
		else if (res.ok === 0)
			toast.err(res.firstError ?? `Could not ${verb.toLowerCase()} any title`);
		else toast.err(`${verb} ${res.ok}, ${res.failed} failed`);
	}

	async function run(
		verb: string,
		fn: (m: Movie) => Promise<unknown>,
		after?: () => void,
	) {
		if (busy) return;
		busy = true;
		try {
			const res = await runBulk(picked, fn);
			qc.invalidateQueries({ queryKey: ["movies"] });
			report(verb, res);
			after?.();
			if (res.failed === 0) onClear();
		} finally {
			busy = false;
		}
	}

	const patch = (m: Movie, body: Record<string, unknown>) =>
		api(`/movies/${m.id}`, { method: "PATCH", body });

	function setMonitored(v: boolean) {
		run(v ? i18n.monitor_monitoring() : i18n.monitor_stopped(), (m) =>
			patch(m, { monitored: v }),
		);
	}
	function searchNow() {
		run("Search dispatched for", (m) =>
			api(`/movies/${m.id}/search-now`, { method: "POST" }),
		);
	}
	function refresh() {
		run("Refresh requested for", (m) =>
			api(`/movies/${m.id}/refresh-metadata`, { method: "POST" }),
		);
	}
	function saveProfile(profile: string) {
		run("Reprofiled", (m) => patch(m, { quality_profile: profile }), () => {
			qpOpen = false;
		});
	}
	function remove(withFiles: boolean) {
		run(
			"Deleted",
			(m) => api(`/movies/${m.id}?delete_files=${withFiles}`, { method: "DELETE" }),
			() => {
				qc.invalidateQueries({ queryKey: ["movies", "counts"] });
				deleteOpen = false;
			},
		);
	}

	let menuItems = $derived<KebabItem[]>([
		{
			key: "refresh",
			label: i18n.action_refresh_metadata(),
			icon: RefreshCw,
			onSelect: refresh,
		},
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
		"inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

	// Phone: three cells and a More sheet. Monitor / Search are the everyday
	// pair; Delete keeps its confirm dialog, so the cell is a route to it, not
	// the deletion itself.
	let touchActions = $derived<TouchAction[]>([
		{
			key: "monitor",
			label: i18n.action_monitor(),
			icon: Bookmark,
			onSelect: () => setMonitored(true),
		},
		{ key: "search", label: i18n.common_search(), icon: Radar, onSelect: searchNow },
	]);

	let touchMenu = $derived<TouchMenuRow[]>([
		{
			key: "monitor",
			label: i18n.action_monitor(),
			icon: Bookmark,
			line: `${monitoredPicked} of ${count} already monitored`,
			onSelect: () => setMonitored(true),
		},
		{
			key: "unmonitor",
			label: i18n.action_stop_monitoring(),
			icon: BookmarkX,
			onSelect: () => setMonitored(false),
		},
		{
			key: "search",
			label: i18n.action_search_releases_for(),
			icon: Radar,
			line: "queues one search per title",
			onSelect: searchNow,
		},
		{
			key: "quality",
			label: i18n.action_change_quality_profile(),
			icon: SlidersHorizontal,
			onSelect: () => (qpOpen = true),
		},
		{
			key: "refresh",
			label: i18n.action_refresh_metadata(),
			icon: RefreshCw,
			onSelect: refresh,
		},
		{
			key: "delete",
			label: i18n.action_remove_from_library(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			line:
				fileCount === 0
					? "no files on disk"
					: `deleting the files frees ${formatBytes(pickedBytes, "0 B")}`,
			onSelect: () => (deleteOpen = true),
		},
	]);
</script>

{#if active}
	<div class="hidden md:block">
		<BulkActionBar {count} {total} {busy} noun="title" {onSelectAll} {onClear}>
		<button
			type="button"
			disabled={busy}
			onclick={() => setMonitored(true)}
			class={btn}
		>
			<Bookmark size={14} aria-hidden="true" />
			{i18n.action_monitor()}
		</button>
		<button
			type="button"
			disabled={busy}
			onclick={() => setMonitored(false)}
			class={btn}
		>
			<BookmarkX size={14} aria-hidden="true" />
			{i18n.action_unmonitor()}
		</button>
		<button type="button" disabled={busy} onclick={searchNow} class={btn}>
			<Radar size={14} aria-hidden="true" />
			{i18n.common_search()}
		</button>
		<button
			type="button"
			disabled={busy}
			onclick={() => (qpOpen = true)}
			class={btn}
		>
			<SlidersHorizontal size={14} aria-hidden="true" />
			{i18n.common_quality()}
		</button>
			<KebabMenu items={menuItems} variant="bar" />
		</BulkActionBar>
	</div>

	<BulkTouchBar
		{count}
		{busy}
		noun="title"
		actions={touchActions}
		menu={touchMenu}
	/>
{/if}

<QualityProfileModal
	open={qpOpen}
	profiles={profilesQuery.data ?? []}
	saving={busy}
	onClose={() => (qpOpen = false)}
	onSave={saveProfile}
/>
<DeleteTitleDialog
	open={deleteOpen}
	title="Remove {plural(count, 'title')} from your library?"
	body="The titles leave your library. Files on disk are kept unless you say otherwise."
	filesLabel="Also delete {plural(fileCount, 'file')} from disk"
	filesNote="Frees {formatBytes(pickedBytes, '0 B')} · cannot be undone."
	canDeleteFiles={fileCount > 0}
	pending={busy}
	onClose={() => (deleteOpen = false)}
	onConfirm={(withFiles) => remove(withFiles)}
/>
