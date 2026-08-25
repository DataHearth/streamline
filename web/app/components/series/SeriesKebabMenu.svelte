<script lang="ts" module>
	export type SeriesAction =
		| "search"
		| "quality"
		| "rename"
		| "refresh"
		| "reidentify"
		| "delete-files"
		| "delete";
</script>

<script lang="ts">
	import {
		Radar,
		Gauge,
		FileEdit,
		RefreshCw,
		Replace,
		Trash2,
	} from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";
	import { auth } from "../../lib/auth.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		onPick,
		disabledActions = [],
		variant = "toolbar",
		allowDeleteFiles = false,
	}: {
		onPick: (a: SeriesAction) => void;
		disabledActions?: SeriesAction[];
		variant?: "toolbar" | "card";
		// "Delete all files" (keep the series, revert to wanted) needs the loaded
		// episode list, so it's only offered from the detail hero, not grid cards.
		allowDeleteFiles?: boolean;
	} = $props();

	const isDisabled = (a: SeriesAction) => disabledActions.includes(a);

	let items = $derived<KebabItem[]>([
		{
			key: "search",
			label: i18n.action_search_wanted_episodes(),
			icon: Radar,
			onSelect: () => onPick("search"),
		},
		{
			key: "quality",
			label: i18n.action_change_quality_profile_ellipsis(),
			icon: Gauge,
			onSelect: () => onPick("quality"),
		},
		{
			key: "rename",
			label: i18n.action_rename_files_ellipsis(),
			icon: FileEdit,
			disabled: isDisabled("rename"),
			title: isDisabled("rename")
				? i18n.series_available_after_import()
				: undefined,
			onSelect: () => onPick("rename"),
		},
		{
			key: "refresh",
			label: i18n.action_refresh_metadata(),
			icon: RefreshCw,
			onSelect: () => onPick("refresh"),
		},
		// Admin-only: the endpoint refuses anyone else, so offering it would be
		// a menu entry that always fails.
		...(auth.isAdmin
			? [
					{
						key: "reidentify",
						label: i18n.action_change_match_ellipsis(),
						icon: Replace,
						onSelect: () => onPick("reidentify"),
					} satisfies KebabItem,
				]
			: []),
		...(allowDeleteFiles
			? [
					{
						key: "delete-files",
						label: i18n.action_delete_all_files(),
						icon: Trash2,
						danger: true,
						dividerBefore: true,
						disabled: isDisabled("delete-files"),
						onSelect: () => onPick("delete-files"),
					} satisfies KebabItem,
				]
			: []),
		{
			// Deleting the files is a checkbox in the confirm, not a second entry
			// one row below the safe one.
			key: "delete",
			label: i18n.action_delete_from_library(),
			icon: Trash2,
			danger: true,
			dividerBefore: !allowDeleteFiles,
			onSelect: () => onPick("delete"),
		},
	]);
</script>

<KebabMenu {items} {variant} />
