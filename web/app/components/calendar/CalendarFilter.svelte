<script lang="ts">
	import { ChevronDown } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import type { CalendarFilter } from "@lib/calendar";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import DropdownOption from "@components/shared/DropdownOption.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Replaces the two independent Movies / Episodes toggles. Two toggles have
	// four states, two of which say the same thing, and seeing only episodes
	// took two taps. One cell per kind, one of them always on. Five cells do not
	// fit beside the month on a phone, so `compact` offers the same choice as a
	// menu under one button that names the current pick.
	let {
		filter = "all",
		compact = false,
		onChange,
	}: {
		filter?: CalendarFilter;
		compact?: boolean;
		onChange: (f: CalendarFilter) => void;
	} = $props();

	// The dot classes are spelled out so the stylesheet build can see them.
	type Cell = { value: CalendarFilter; label: string; dot?: string; dotClass?: string };
	const ALL_CELL: Cell = { value: "all", label: i18n.common_all() };
	const CELLS: Cell[] = [
		ALL_CELL,
		{ value: "movies", label: i18n.movies_label(), dot: "var(--kind-movie)", dotClass: "bg-[var(--kind-movie)]" },
		{ value: "episodes", label: i18n.series_episodes(), dot: "var(--kind-episode)", dotClass: "bg-[var(--kind-episode)]" },
		{ value: "albums", label: i18n.common_albums(), dot: "var(--kind-album)", dotClass: "bg-[var(--kind-album)]" },
		{ value: "books", label: i18n.books_label(), dot: "var(--kind-book)", dotClass: "bg-[var(--kind-book)]" },
	];

	const cell =
		"inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-sm px-2 py-1.5 text-[12px] font-medium transition md:px-3 md:text-[12.5px] lg:min-h-0 lg:min-w-0";

	let current = $derived(CELLS.find((c) => c.value === filter) ?? ALL_CELL);
	let open = $state(false);
	let trigger = $state<HTMLButtonElement | null>(null);
</script>

{#if compact}
	<button
		bind:this={trigger}
		type="button"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-label="{i18n.calendar_filter_releases()}: {current.label}"
		onclick={() => (open = !open)}
		class={cn(
			"inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-md border px-3 text-[12.5px] font-medium transition",
			filter === "all"
				? "border-border bg-bg-elevated text-fg"
				: "border-accent-line bg-accent-soft text-accent-text",
		)}
	>
		{#if current.dot}
			<span class="h-1.5 w-1.5 shrink-0 rounded-full" style:background-color={current.dot} aria-hidden="true"></span>
		{/if}
		{current.label}
		<ChevronDown size={14} class="shrink-0 text-fg-subtle" aria-hidden="true" />
	</button>
	<DropdownMenu
		{open}
		anchor={trigger}
		onClose={() => (open = false)}
		align="end"
		minWidth="11rem"
		ariaLabel={i18n.calendar_filter_releases()}
	>
		{#each CELLS as c (c.value)}
			<DropdownOption
				label={c.label}
				dot={c.dotClass}
				selected={filter === c.value}
				onSelect={() => {
					onChange(c.value);
					open = false;
				}}
			/>
		{/each}
	</DropdownMenu>
{:else}
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
{/if}
