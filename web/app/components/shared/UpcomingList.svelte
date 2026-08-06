<script lang="ts">
	import { Calendar } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import StatusPill from "./StatusPill.svelte";
	import type { CalendarEvent } from "../../lib/calendar";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { getLocale } from "../../lib/paraglide/runtime.js";

	let {
		events,
		title,
		seeAllHref,
		seeAllLabel = i18n.upcoming_see_all(),
		emptyText = i18n.upcoming_empty_hint(),
		stretch = false,
	}: {
		events: CalendarEvent[];
		title: string;
		seeAllHref?: string;
		seeAllLabel?: string;
		emptyText?: string;
		// Fill the grid row instead of hugging its content — the dashboard pairs
		// this with the Wanted rail and the two should square off.
		stretch?: boolean;
	} = $props();

	const monthFmt = new Intl.DateTimeFormat(getLocale(), { month: "short" });

	function stamp(d: Date): { day: string; month: string } {
		if (Number.isNaN(d.getTime())) return { day: "—", month: "" };
		return {
			day: String(d.getDate()).padStart(2, "0"),
			// The 44px stamp cell fits four mono glyphs; French short months carry a
			// trailing period ("janv.") that would spend one of them on punctuation.
			month: monthFmt.format(d).replace(".", "").toUpperCase().slice(0, 4),
		};
	}

	function daysUntil(d: Date): string {
		const target = d.getTime();
		if (Number.isNaN(target)) return "";
		const days = Math.max(0, Math.round((target - Date.now()) / 86_400_000));
		return days === 0 ? i18n.lc_today() : i18n.upcoming_in_days({ days });
	}
</script>

<aside
	class={cn(
		"rounded-lg border border-border bg-bg-elevated p-4",
		stretch ? "h-full" : "self-start",
	)}
	aria-label={title}
>
	<header class="mb-3 flex items-baseline justify-between">
		<h2 class="text-base font-semibold tracking-tight text-fg">{title}</h2>
		{#if seeAllHref}
			<a
				href={seeAllHref}
				class="text-[11.5px] text-fg-subtle transition hover:text-accent-text"
			>
				{seeAllLabel}
			</a>
		{/if}
	</header>

	{#if events.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-1.5 px-2 py-6 text-center"
		>
			<Calendar size={22} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">{i18n.upcoming_nothing_scheduled()}</p>
			<p class="text-xs text-fg-muted">{emptyText}</p>
		</div>
	{:else}
		<ul class="flex flex-col gap-1">
			{#each events as ev (ev.id)}
				{@const date = stamp(ev.date)}
				{@const when = daysUntil(ev.date)}
				<li>
					<a
						href={ev.href}
						class="grid grid-cols-[44px_1fr_auto] items-center gap-3 rounded-md px-1.5 py-2.5 transition hover:bg-surface"
					>
						<span
							class="grid place-items-center rounded-md border border-border bg-surface py-1.5 text-center"
						>
							<span class="font-mono text-[17px] font-bold tabular leading-none text-fg">
								{date.day}
							</span>
							<span class="mt-1 font-mono text-[9px] tracking-[0.12em] text-fg-faint">
								{date.month}
							</span>
						</span>
						<div class="min-w-0">
							<div class="truncate text-[13px] font-medium text-fg">
								{ev.title}
							</div>
							<div class="mt-0.5 font-mono text-[10.5px] text-fg-subtle">
								{ev.subtitle ?? i18n.lc_digital_release()}{when ? ` · ${when}` : ""}
							</div>
						</div>
						<StatusPill status={ev.status} size="sm" />
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</aside>
