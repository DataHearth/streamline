<script lang="ts">
	import { Radar, Gauge, FileEdit, RefreshCw, Trash2 } from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";

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
			label: "Search for releases",
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
				? "Available after the movie has been imported"
				: undefined,
			onSelect: () => onPick("rename"),
		},
		{
			key: "refresh",
			label: "Refresh metadata",
			icon: RefreshCw,
			onSelect: () => onPick("refresh"),
		},
		{
			// Deleting the files is a checkbox in the confirm, not a second entry
			// one row below the safe one.
			key: "delete",
			label: "Delete from library…",
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => onPick("delete"),
		},
	]);
</script>

<KebabMenu {items} {variant} />
