<script lang="ts">
	import { cn } from "../../lib/cn";
	import type { CalendarFilter } from "../../lib/calendar";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Replaces the two independent Movies / Episodes toggles. Two toggles have
	// four states, two of which say the same thing, and seeing only episodes
	// took two taps. Three cells, one of them always on.
	let {
		filter = "all",
		onChange,
	}: { filter?: CalendarFilter; onChange: (f: CalendarFilter) => void } =
		$props();

	const CELLS: { value: CalendarFilter; label: string; dot?: string }[] = [
		{ value: "all", label: i18n.common_all() },
		{ value: "movies", label: i18n.movies_label(), dot: "var(--kind-movie)" },
		{ value: "episodes", label: i18n.series_episodes(), dot: "var(--kind-episode)" },
	];

	const cell =
		"inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-sm px-2 py-1.5 text-[12px] font-medium transition md:px-3 md:text-[12.5px] lg:min-h-0 lg:min-w-0";
</script>

<div
	class="flex shrink-0 items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
	role="group"
	aria-label={i18n.calendar_filter_releases()}
>
	{#each CELLS as c (c.value)}
		<button
			type="button"
			onclick={() => onChange(c.value)}
			aria-pressed={filter === c.value}
			class={cn(
				cell,
				filter === c.value
					? "bg-accent-soft text-accent-text"
					: "text-fg-subtle hover:text-fg",
			)}
		>
			{#if c.dot}
				<span
					class="h-1.5 w-1.5 shrink-0 rounded-full"
					style:background-color={c.dot}
					aria-hidden="true"
				></span>
			{/if}
			{c.label}
		</button>
	{/each}
</div>
