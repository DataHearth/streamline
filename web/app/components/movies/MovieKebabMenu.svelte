<script lang="ts">
	import { Search, Gauge, FileEdit, RefreshCw, Trash2 } from "@lucide/svelte";
	import KebabMenu, { type KebabItem } from "../shared/KebabMenu.svelte";

	type Action =
		| "search"
		| "quality"
		| "rename"
		| "refresh"
		| "delete"
		| "delete-with-files";

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
			icon: Search,
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
			key: "delete",
			label: "Delete from library",
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			onSelect: () => onPick("delete"),
		},
		{
			key: "delete-with-files",
			label: "Delete + files",
			icon: Trash2,
			danger: true,
			disabled: isDisabled("delete-with-files"),
			onSelect: () => onPick("delete-with-files"),
		},
	]);
</script>

<KebabMenu {items} {variant} />
