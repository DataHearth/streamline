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
	import type { TVShow, QualityProfile } from "../../lib/types";

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
		if (res.failed === 0)
			toast.ok(`${verb} ${plural(res.ok, "series", "series")}`);
		else if (res.ok === 0)
			toast.err(res.firstError ?? `Could not ${verb.toLowerCase()} any series`);
		else toast.err(`${verb} ${res.ok}, ${res.failed} failed`);
	}

	async function run(
		verb: string,
		fn: (s: TVShow) => Promise<unknown>,
		after?: () => void,
	) {
		if (busy) return;
		busy = true;
		try {
			const res = await runBulk(picked, fn);
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
		run(v ? "Monitoring" : "Stopped monitoring", (s) =>
			patch(s, { monitored: v }),
		);
	}
	function searchNow() {
		run("Search dispatched for", (s) =>
			api(`/series/${s.id}/search-now`, { method: "POST" }),
		);
	}
	function refresh() {
		run("Refresh requested for", (s) =>
			api(`/series/${s.id}/refresh-metadata`, { method: "POST" }),
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
			key: "refresh",
			label: "Refresh metadata",
			icon: RefreshCw,
			onSelect: refresh,
		},
		{
			key: "delete",
			label: "Remove from library…",
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => (deleteOpen = true),
		},
	]);

	const btn =
		"inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-border bg-bg-elevated px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50";

	let touchActions = $derived<TouchAction[]>([
		{
			key: "monitor",
			label: "Monitor",
			icon: Bookmark,
			onSelect: () => setMonitored(true),
		},
		{ key: "search", label: "Search", icon: Radar, onSelect: searchNow },
	]);

	let touchMenu = $derived<TouchMenuRow[]>([
		{
			key: "monitor",
			label: "Monitor",
			icon: Bookmark,
			line: `${monitoredPicked} of ${count} already monitored`,
			onSelect: () => setMonitored(true),
		},
		{
			key: "unmonitor",
			label: "Stop monitoring",
			icon: BookmarkX,
			onSelect: () => setMonitored(false),
		},
		{
			key: "search",
			label: "Search for wanted episodes",
			icon: Radar,
			line: wantedCount
				? `${plural(wantedCount, "episode")} wanted`
				: "nothing wanted right now",
			onSelect: searchNow,
		},
		{
			key: "quality",
			label: "Change quality profile",
			icon: SlidersHorizontal,
			onSelect: () => (qpOpen = true),
		},
		{
			key: "refresh",
			label: "Refresh metadata",
			icon: RefreshCw,
			onSelect: refresh,
		},
		{
			key: "delete",
			label: "Remove from library…",
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
			Monitor
		</button>
		<button
			type="button"
			disabled={busy}
			onclick={() => setMonitored(false)}
			class={btn}
		>
			<BookmarkX size={14} aria-hidden="true" />
			Unmonitor
		</button>
		<button type="button" disabled={busy} onclick={searchNow} class={btn}>
			<Radar size={14} aria-hidden="true" />
			Search
		</button>
		<button
			type="button"
			disabled={busy}
			onclick={() => (qpOpen = true)}
			class={btn}
		>
			<SlidersHorizontal size={14} aria-hidden="true" />
			Quality
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
