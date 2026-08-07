<script lang="ts">
	import { Film, Tv } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import Poster from "../movies/Poster.svelte";
	import EventDot from "./EventDot.svelte";
	import { dotToken, type CalendarEvent } from "../../lib/calendar";

	let {
		event,
		size = "sm",
	}: { event: CalendarEvent; size?: "sm" | "md" } = $props();

	// Everything after the episode number: the clock, then the episode title.
	// A movie has neither, only its reason for being here.
	let tail = $derived(
		[event.time, event.detail].filter(Boolean).join(" · "),
	);
</script>

<a
	href={event.href}
	class={cn(
		"grid items-center gap-3 rounded-md px-3 transition hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
		size === "md"
			? "grid-cols-[46px_1fr_auto] gap-3.5 py-2.5"
			: "grid-cols-[38px_1fr_auto] py-2",
	)}
>
	<span
		class="relative grid aspect-[2/3] w-full place-items-center overflow-hidden rounded border border-border bg-bg-card text-fg-faint"
	>
		{#if event.kind === "movie"}
			<Film size={14} aria-hidden="true" />
		{:else}
			<Tv size={14} aria-hidden="true" />
		{/if}
		<Poster
			src={event.poster}
			alt=""
			class="absolute inset-0 h-full w-full object-cover"
		/>
	</span>

	<span class="min-w-0">
		<span
			class="block truncate text-[14px] font-semibold tracking-[-0.01em] text-fg"
		>
			{event.title}
		</span>
		<span class="mt-0.5 block truncate font-mono text-[11px] text-fg-subtle">
			{#if event.subtitle}<span class="text-fg-muted">{event.subtitle}</span>{/if}{#if event.subtitle && tail}<span aria-hidden="true">&nbsp;·&nbsp;</span>{/if}{tail}
		</span>
	</span>

	<!-- One treatment for both kinds. This used to be a "Wanted" pill on movies
	     and a bare dot on episodes, so the trailing column meant status on some
	     rows and kind on others. The dot is kind only — the same amber/purple
	     the filter switch and the grid chips use. A bare colour cannot carry
	     state without a label; the Upcoming list's pill does that. -->
	<EventDot status={dotToken(event)} size="md" />
</a>
