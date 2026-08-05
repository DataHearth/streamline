<script lang="ts">
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import {
		Bookmark,
		BookmarkX,
		Search,
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
	import Dialog from "../modals/Dialog.svelte";
	import type { Movie, QualityProfile } from "../../lib/types";

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
	let fileCount = $derived(
		picked.reduce((n, m) => n + (m.media_files?.length ?? 0), 0),
	);
	let pickedBytes = $derived(
		picked.reduce(
			(n, m) => n + (m.media_files ?? []).reduce((s, f) => s + f.size, 0),
			0,
		),
	);
	let monitoredPicked = $derived(picked.filter((m) => m.monitored).length);
	// The selection is off-screen as often as not on a phone, so the sheet says
	// which titles it is about to act on.
	let pickedNames = $derived(
		picked.length === 0
			? ""
			: picked
					.slice(0, 2)
					.map((m) => m.title)
					.join(", ") + (picked.length > 2 ? `, +${picked.length - 2}` : ""),
	);

	let qpOpen = $state(false);
	let deleteOpen = $state(false);
	let deleteWithFilesOpen = $state(false);
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
		run(v ? "Monitoring" : "Stopped monitoring", (m) =>
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
				deleteWithFilesOpen = false;
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
			label: "Remove from library",
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => (deleteOpen = true),
		},
		{
			key: "delete-with-files",
			label: "Remove + delete files",
			icon: Trash2,
			danger: true,
			disabled: fileCount === 0,
			title: fileCount === 0 ? "No files on disk" : undefined,
			onSelect: () => (deleteWithFilesOpen = true),
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
			label: "Monitor",
			icon: Bookmark,
			onSelect: () => setMonitored(true),
		},
		{ key: "search", label: "Search", icon: Search, onSelect: searchNow },
		{
			key: "delete",
			label: "Delete",
			icon: Trash2,
			danger: true,
			onSelect: () => (deleteOpen = true),
		},
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
			label: "Search for releases",
			icon: Search,
			line: "queues one search per title",
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
			label: "Remove from library",
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			line: "keeps files on disk",
			onSelect: () => (deleteOpen = true),
		},
		{
			key: "delete-with-files",
			label: "Remove and delete files",
			icon: Trash2,
			danger: true,
			disabled: fileCount === 0,
			line:
				fileCount === 0
					? "no files on disk"
					: `frees ${formatBytes(pickedBytes, "0 B")} · cannot be undone`,
			onSelect: () => (deleteWithFilesOpen = true),
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
			<Search size={14} aria-hidden="true" />
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
		{total}
		{busy}
		noun="title"
		actions={touchActions}
		menu={touchMenu}
		footer={pickedNames}
		{onSelectAll}
		{onClear}
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
	open={deleteOpen}
	title="Remove {plural(count, 'title')} from your library?"
	body="Files on disk will be kept."
	onClose={() => (deleteOpen = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete",
			variant: "danger",
			dismiss: false,
			pending: busy,
			onClick: () => remove(false),
		},
	]}
/>
<Dialog
	open={deleteWithFilesOpen}
	title="Remove {plural(count, 'title')} and delete their files?"
	body="{plural(fileCount, 'file')} will be deleted from disk. This cannot be undone."
	onClose={() => (deleteWithFilesOpen = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete + files",
			variant: "danger",
			dismiss: false,
			pending: busy,
			onClick: () => remove(true),
		},
	]}
/>
