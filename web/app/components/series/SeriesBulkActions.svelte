<script lang="ts">
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import {
		Bookmark,
		BookmarkX,
		Radar,
		SlidersHorizontal,
		FileEdit,
		RefreshCw,
		Trash2,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { runBulk, plural } from "../../lib/bulk";
	import BulkActionBar from "../shared/BulkActionBar.svelte";
	import BulkTouchBar from "../shared/BulkTouchBar.svelte";
	import type {
		TouchAction,
		TouchMenuRow,
	} from "../shared/BulkTouchBar.svelte";
	import KebabMenu from "../shared/KebabMenu.svelte";
	import type { KebabItem } from "../shared/KebabMenu.svelte";
	import QualityProfileModal from "../movies/QualityProfileModal.svelte";
	import DeleteTitleDialog from "../shared/DeleteTitleDialog.svelte";
	import Dialog from "../modals/Dialog.svelte";
	import type { TVShow, QualityProfile } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		series,
		selected,
		total,
		onSelectAll,
		onClear,
	}: {
		series: TVShow[];
		selected: Set<number>;
		total: number;
		onSelectAll: () => void;
		onClear: () => void;
	} = $props();

	let count = $derived(selected.size);
	let active = $derived(count > 0);
	let picked = $derived(series.filter((s) => selected.has(s.id)));
	let episodeCount = $derived(
		picked.reduce((n, s) => n + (s.have_episodes ?? 0), 0),
	);
	let wantedCount = $derived(
		picked.reduce((n, s) => n + (s.wanted_episodes ?? 0), 0),
	);
	let monitoredPicked = $derived(picked.filter((s) => s.monitored).length);
	// A show with no episode on disk can only come back with an empty rename
	// plan, so it is dropped from the request set rather than counted in the
	// toast. have_episodes is the list rollup; the episode tree is detail-only.
	let renamable = $derived(picked.filter((s) => (s.have_episodes ?? 0) > 0));
	let qpOpen = $state(false);
	let deleteOpen = $state(false);
	let renameOpen = $state(false);
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
		if (res.failed === 0)
			toast.ok(`${verb} ${plural(res.ok, "series", "series")}`);
		else if (res.ok === 0)
			toast.err(res.firstError ?? `Could not ${verb.toLowerCase()} any series`);
		else toast.err(`${verb} ${res.ok}, ${res.failed} failed`);
	}

	// items defaults to the whole selection; rename passes the subset that has
	// episodes on disk, so the count the toast reports is what was acted on.
	async function run(
		verb: string,
		fn: (s: TVShow) => Promise<unknown>,
		after?: () => void,
		items: TVShow[] = picked,
	) {
		if (busy) return;
		busy = true;
		try {
			const res = await runBulk(items, fn);
			qc.invalidateQueries({ queryKey: ["series"] });
			report(verb, res);
			after?.();
			if (res.failed === 0) onClear();
		} finally {
			busy = false;
		}
	}

	const patch = (s: TVShow, body: Record<string, unknown>) =>
		api(`/series/${s.id}`, { method: "PATCH", body });

	function setMonitored(v: boolean) {
		run(v ? i18n.monitor_monitoring() : i18n.monitor_stopped(), (s) =>
			patch(s, { monitored: v }),
		);
	}
	function searchNow() {
		run("Search dispatched for", (s) =>
			api(`/series/${s.id}/search`, { method: "POST" }),
		);
	}
	function refresh() {
		run("Refresh requested for", (s) =>
			api(`/series/${s.id}/refresh-metadata`, { method: "POST" }),
		);
	}
	// No preview: one per selected show is a request each to render a list
	// nobody can read at that length, and a show's plan is every episode it
	// holds. The single-show kebab keeps its preview for when the moves matter.
	function renameFiles() {
		run(
			"Renamed",
			(s) => api(`/series/${s.id}/rename`, { method: "POST" }),
			() => (renameOpen = false),
			renamable,
		);
	}
	function saveProfile(profile: string) {
		run("Reprofiled", (s) => patch(s, { quality_profile: profile }), () => {
			qpOpen = false;
		});
	}
	function remove(withFiles: boolean) {
		run(
			"Deleted",
			(s) => api(`/series/${s.id}?delete_files=${withFiles}`, { method: "DELETE" }),
			() => {
				qc.invalidateQueries({ queryKey: ["series", "counts"] });
				deleteOpen = false;
			},
		);
	}

	let menuItems = $derived<KebabItem[]>([
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: renamable.length === 0,
			title:
				renamable.length === 0
					? "Available once an episode has been imported"
					: undefined,
			onSelect: () => (renameOpen = true),
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
			onSelect: () => (deleteOpen = true),
		},
	]);

	const btn =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

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
			label: i18n.action_search_wanted_episodes(),
			icon: Radar,
			line: wantedCount
				? `${plural(wantedCount, "episode")} wanted`
				: "nothing wanted right now",
			onSelect: searchNow,
		},
		{
			key: "quality",
			label: i18n.action_change_quality_profile(),
			icon: SlidersHorizontal,
			onSelect: () => (qpOpen = true),
		},
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: renamable.length === 0,
			line:
				renamable.length === 0
					? "no episodes on disk"
					: `${plural(renamable.length, "series", "series")} with episodes on disk`,
			onSelect: () => (renameOpen = true),
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
				episodeCount === 0
					? "no episodes on disk"
					: `${plural(episodeCount, "episode")} on disk`,
			onSelect: () => (deleteOpen = true),
		},
	]);
</script>

{#if active}
	<div class="hidden md:block">
		<BulkActionBar
		{count}
		{total}
		{busy}
		noun="series"
		nounPlural="series"
		{onSelectAll}
		{onClear}
	>
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
		noun="series"
		nounPlural="series"
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
<Dialog
	open={renameOpen}
	title="Rename files for {plural(renamable.length, 'series', 'series')}?"
	body="Every episode on disk is moved to match your naming template. Files already sitting at their target name are left alone."
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
	title="Remove {count} series from your library?"
	body="The series leave your library. Files on disk are kept unless you say otherwise."
	filesLabel="Also delete {plural(episodeCount, 'episode')} from disk"
	filesNote="This cannot be undone."
	canDeleteFiles={episodeCount > 0}
	pending={busy}
	onClose={() => (deleteOpen = false)}
	onConfirm={(withFiles) => remove(withFiles)}
/>
