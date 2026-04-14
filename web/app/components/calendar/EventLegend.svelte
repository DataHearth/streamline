<script lang="ts">
	import EventDot from "./EventDot.svelte";

	type LegendItem = {
		status: "wanted" | "available" | "grabbing" | "missing";
		label: string;
	};

	const items: LegendItem[] = [
		{ status: "available", label: "Released / available" },
		{ status: "grabbing", label: "Grabbing" },
		{ status: "wanted", label: "Wanted" },
		{ status: "missing", label: "Missing" },
	];
</script>

<ul
	aria-label="Calendar legend"
	class="flex flex-wrap items-center gap-x-5 gap-y-2 text-[12px] text-fg-muted"
>
	{#each items as item (item.status)}
		<li class="flex items-center gap-2">
			<EventDot status={item.status} size="md" />
			<span>{item.label}</span>
		</li>
	{/each}
</ul>
