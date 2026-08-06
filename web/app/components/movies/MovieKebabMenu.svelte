<script lang="ts">
	import { Radar, Gauge, FileEdit, RefreshCw, Trash2 } from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Action =
		| "search"
		| "quality"
		| "rename"
		| "refresh"
		| "delete";

	let {
		onPick,
		disabledActions = [],
		variant = "toolbar",
	}: {
		onPick: (a: Action) => void;
		disabledActions?: Action[];
		variant?: "toolbar" | "card";
	} = $props();

	const isDisabled = (a: Action) => disabledActions.includes(a);

	let items = $derived<KebabItem[]>([
		{
			key: "search",
			label: i18n.action_search_releases_for(),
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
				? i18n.movies_available_after_import()
				: undefined,
			onSelect: () => onPick("rename"),
		},
		{
			key: "refresh",
			label: i18n.action_refresh_metadata(),
			icon: RefreshCw,
			onSelect: () => onPick("refresh"),
		},
		{
			// Deleting the files is a checkbox in the confirm, not a second entry
			// one row below the safe one.
			key: "delete",
			label: i18n.action_delete_from_library(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => onPick("delete"),
		},
	]);
</script>

<KebabMenu {items} {variant} />
