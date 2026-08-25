<script lang="ts">
	import { Calendar } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { dotToken, type CalendarEvent } from "../../lib/calendar";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { getLocale } from "../../lib/paraglide/runtime.js";

	let {
		events,
		title,
		seeAllHref,
		seeAllLabel = i18n.upcoming_see_all(),
		emptyText = i18n.upcoming_empty_hint(),
		stretch = false,
		fill = false,
	}: {
		events: CalendarEvent[];
		title: string;
		seeAllHref?: string;
		seeAllLabel?: string;
		emptyText?: string;
		// Fill the grid row instead of hugging its content — the dashboard pairs
		// this with the Wanted rail and the two should square off.
		stretch?: boolean;
		// Take the height the container gives and scroll the list inside it, for a
		// host that is itself clamped to the viewport (the calendar's side panel).
		fill?: boolean;
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
		fill
			? "flex h-full min-h-0 flex-col"
			: stretch
				? "h-full"
				: "self-start",
	)}
	aria-label={title}
>
	<header class="mb-3 flex flex-none items-baseline justify-between">
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
		<ul
			class={cn(
				"flex flex-col gap-1",
				fill && "-mr-2 min-h-0 flex-1 overflow-y-auto pr-2",
			)}
		>
			{#each events as ev (ev.id)}
				{@const date = stamp(ev.date)}
				{@const when = daysUntil(ev.date)}
				<!-- The meta line, in falling order of value: the episode number, the
				     episode's own title, then how far off it is. A movie has only its
				     reason for being here. `when` sits last because it is the one
				     segment the date stamp already states, so it is the one truncation
				     should eat first. -->
				<!-- The fallback is the movie's reason for being here, so it is the
				     movie's alone: an episode TVDB has not titled yet has no detail
				     either, and inheriting this one billed it as a digital release. -->
				{@const meta = [
					ev.subtitle,
					ev.kind === "movie"
						? (ev.detail ?? i18n.lc_digital_release())
						: ev.detail,
					when,
				]
					.filter(Boolean)
					.join(" · ")}
				<li>
					<a
						href={ev.href}
						title="{ev.title} · {meta}"
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
							<div class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
								{meta}
							</div>
						</div>
						<!-- Kind, not status. Everything on this list is unreleased and
						     wanted, so a status pill printed the same word down the whole
						     panel; what the row does not otherwise say is whether it is a
						     film or an episode. `--kind-*` are the calendar's own colours,
						     deliberately outside the status ramp. -->
						<span
							class="kind-pill shrink-0 whitespace-nowrap rounded-full border px-1.5 py-[1px] text-[10px] font-semibold tracking-[0.02em]"
							style:--c="var(--kind-{dotToken(ev)})"
						>
							{ev.kind === "movie" ? i18n.common_movie() : i18n.common_episode()}
						</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</aside>

<style>
	.kind-pill {
		background-color: var(--c);
		border-color: var(--c);
		color: var(--bg-deep);
	}
</style>
