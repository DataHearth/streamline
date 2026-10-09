<script lang="ts">
	import { onMount } from "svelte";
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { goto } from "@roxi/routify";
	import { FileEdit, Gauge, Radar, RefreshCw, Trash2 } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { QualityProfile } from "@lib/types";
	import { profilesPath } from "@lib/music-books";
	import KebabMenu, { type KebabItem } from "./KebabMenu.svelte";
	import QualityProfileModal from "./QualityProfileModal.svelte";
	import DeleteTitleDialog from "./DeleteTitleDialog.svelte";
	import RenamePreviewModal from "./RenamePreviewModal.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The kebab for an artist, a book or a book series (detail hero, library row
	// or shelf card): the entries
	// a movie or a series carries, run against `base` (e.g. /music/artists/3).
	// No "Change match" — there is no provider rematch for these yet. Quality
	// profiles are the medium's own, never the video ones.
	let {
		base,
		title,
		media,
		queryKey,
		backHref,
		hasFiles,
		profile,
		searchLabel,
		onSearch,
		removeBody,
		filesLabel,
		canRemove = true,
		variant = "hero",
	}: {
		base: string;
		title: string;
		media: "music" | "books";
		queryKey: string;
		backHref: string;
		hasFiles: boolean;
		profile?: string;
		searchLabel?: string;
		onSearch?: () => void;
		removeBody: string;
		filesLabel: string;
		canRemove?: boolean;
		variant?: "hero" | "row" | "tile" | "card";
	} = $props();

	let navigate = $state<(p: string) => void>(() => {});
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let pending = $state(false);

	const qc = useQueryClient();
	const invalidate = () => qc.invalidateQueries({ queryKey: [queryKey] });
	const profiles = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles", media],
		queryFn: () => api<QualityProfile[]>(profilesPath(media)),
		enabled: qpOpen,
	}));

	async function run(fn: () => Promise<void>, fail: string) {
		pending = true;
		try {
			await fn();
		} catch (e) {
			toast.err(errorText(e, fail));
		} finally {
			pending = false;
		}
	}
	const saveProfile = (p: string) =>
		run(async () => {
			await api(base, { method: "PATCH", body: { quality_profile: p } });
			invalidate();
			toast.ok(i18n.quality_updated());
			qpOpen = false;
		}, i18n.common_update_failed());
	const refresh = () =>
		run(async () => {
			await api(`${base}/refresh-metadata`, { method: "POST" });
			invalidate();
			toast.ok(i18n.series_refresh_requested());
		}, i18n.common_refresh_failed());
	const remove = (withFiles: boolean) =>
		run(async () => {
			await api(`${base}?delete_files=${withFiles}`, { method: "DELETE" });
			deleteOpen = false;
			toast.ok(i18n.library_removed({ title }));
			navigate(backHref);
			invalidate();
		}, i18n.common_delete_failed());

	let items = $derived<KebabItem[]>([
		...(onSearch && searchLabel ? [{ key: "search", label: searchLabel, icon: Radar, onSelect: onSearch } satisfies KebabItem] : []),
		{ key: "quality", label: i18n.action_change_quality_profile_ellipsis(), icon: Gauge, onSelect: () => (qpOpen = true) },
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: !hasFiles,
			title: hasFiles ? undefined : i18n.library_available_after_import(),
			onSelect: () => (renameOpen = true),
		},
		{ key: "refresh", label: i18n.action_refresh_metadata(), icon: RefreshCw, onSelect: refresh },
		// One destructive entry: whether the files go too is the confirm's
		// checkbox, not a second item beside this one.
		...(canRemove
			? [{ key: "delete", label: i18n.action_delete_from_library(), icon: Trash2, danger: true, dividerBefore: true, onSelect: () => (deleteOpen = true) } satisfies KebabItem]
			: []),
	]);
</script>

<KebabMenu {items} {variant} />

<QualityProfileModal
	open={qpOpen}
	current={profile}
	profiles={profiles.data ?? []}
	saving={pending}
	onClose={() => (qpOpen = false)}
	onSave={saveProfile}
/>
<RenamePreviewModal open={renameOpen} path={base} {queryKey} onClose={() => (renameOpen = false)} />
<DeleteTitleDialog
	open={deleteOpen}
	title={i18n.series_remove_title({ title })}
	body={removeBody}
	{filesLabel}
	filesNote={i18n.common_cannot_undo()}
	canDeleteFiles={hasFiles}
	{pending}
	onClose={() => (deleteOpen = false)}
	onConfirm={remove}
/>
