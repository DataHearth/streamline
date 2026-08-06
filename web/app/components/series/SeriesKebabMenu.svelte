<script lang="ts" module>
	export type SeriesAction =
		| "search"
		| "quality"
		| "rename"
		| "refresh"
		| "delete-files"
		| "delete";
</script>

<script lang="ts">
	import { Radar, Gauge, FileEdit, RefreshCw, Trash2 } from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";

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
			label: "Search for wanted episodes",
			icon: Radar,
			onSelect: () => onPick("search"),
		},
		{
			key: "quality",
			label: "Change quality profile…",
			icon: Gauge,
			onSelect: () => onPick("quality"),
		},
		{
			key: "rename",
			label: "Rename files…",
			icon: FileEdit,
			disabled: isDisabled("rename"),
			title: isDisabled("rename")
				? "Available once episodes have been imported"
				: undefined,
			onSelect: () => onPick("rename"),
		},
		{
			key: "refresh",
			label: "Refresh metadata",
			icon: RefreshCw,
			onSelect: () => onPick("refresh"),
		},
		...(allowDeleteFiles
			? [
					{
						key: "delete-files",
						label: "Delete all files",
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
			label: "Delete from library…",
			icon: Trash2,
			danger: true,
			dividerBefore: !allowDeleteFiles,
			onSelect: () => onPick("delete"),
		},
	]);
</script>

<KebabMenu {items} {variant} />
