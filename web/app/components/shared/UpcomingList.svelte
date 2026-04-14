<script lang="ts">
	import { Calendar } from "@lucide/svelte";
	import StatusPill from "./StatusPill.svelte";
	import type { UpcomingMovie } from "../../lib/types";

	let {
		events,
		title,
		seeAllHref,
		seeAllLabel = "Calendar →",
		emptyText = "Wanted movies with a digital release will appear here.",
	}: {
		events: UpcomingMovie[];
		title: string;
		seeAllHref?: string;
		seeAllLabel?: string;
		emptyText?: string;
	} = $props();

	const MONTHS = [
		"JAN",
		"FEB",
		"MAR",
		"APR",
		"MAY",
		"JUN",
		"JUL",
		"AUG",
		"SEP",
		"OCT",
		"NOV",
		"DEC",
	];

	function stamp(iso: string | undefined): { day: string; month: string } {
		if (!iso) return { day: "—", month: "" };
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return { day: "—", month: "" };
		return {
			day: String(d.getDate()).padStart(2, "0"),
			month: MONTHS[d.getMonth()] ?? "",
		};
	}

	function daysUntil(iso: string | undefined): string {
		if (!iso) return "";
		const target = new Date(iso).getTime();
		if (Number.isNaN(target)) return "";
		const days = Math.max(0, Math.round((target - Date.now()) / 86_400_000));
		return days === 0 ? "today" : `in ${days}d`;
	}
</script>

<aside
	class="self-start rounded-lg border border-border bg-bg-elevated p-4"
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
			<p class="text-sm font-medium text-fg">Nothing scheduled</p>
			<p class="text-xs text-fg-muted">{emptyText}</p>
		</div>
	{:else}
		<ul class="flex flex-col gap-1">
			{#each events as ev (ev.id)}
				{@const date = stamp(ev.digital_release_date)}
				{@const when = daysUntil(ev.digital_release_date)}
				<li>
					<a
						href="/movies/{ev.id}"
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
								digital release{when ? ` · ${when}` : ""}
							</div>
						</div>
						<StatusPill status="wanted" size="sm" />
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</aside>
