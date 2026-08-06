<script lang="ts">
	import { CalendarDays } from "@lucide/svelte";
	import {
		dayLabel,
		groupByDay,
		isToday,
		type CalendarEvent,
	} from "../../lib/calendar";
	import EventRow from "./EventRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		events,
		size = "sm",
		emptyText = i18n.upcoming_agenda_empty_hint(),
	}: {
		events: CalendarEvent[];
		size?: "sm" | "md";
		emptyText?: string;
	} = $props();

	let groups = $derived(groupByDay(events));
</script>

{#if groups.length === 0}
	<div class="flex flex-col items-center justify-center gap-1.5 px-4 py-10 text-center">
		<CalendarDays size={22} class="text-fg-faint" aria-hidden="true" />
		<p class="text-sm font-medium text-fg">{i18n.upcoming_nothing_scheduled()}</p>
		<p class="max-w-[36ch] text-xs text-fg-muted">{emptyText}</p>
	</div>
{:else}
	<div class="flex flex-col">
		{#each groups as group (group.key)}
			<!-- The date header stays put while its own events scroll past it, so
			     the day a row belongs to is always on screen. -->
			<div
				class="sticky top-0 z-10 flex items-center gap-2.5 bg-bg-deep px-3 pb-1.5 pt-4"
			>
				<span
					class="font-mono text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>
					{dayLabel(group.date)}
				</span>
				{#if isToday(group.date)}
					<span
						class="rounded-full bg-accent px-[7px] py-[2px] font-mono text-[9.5px] font-semibold uppercase tracking-[0.12em] text-fg-on-accent"
					>
						{i18n.common_today()}
					</span>
				{/if}
				<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
			</div>
			{#each group.events as event (event.id)}
				<EventRow {event} {size} />
			{/each}
		{/each}
	</div>
{/if}
