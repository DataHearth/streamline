<script lang="ts">
	import { formatDateShort } from "@lib/dates";
	import { fmtCount, sumDays, type StatsDay } from "@lib/stats";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One column per day, its height the day's grabs. Inside it, from the
	// bottom: what was imported, then what failed; the rest of the column is
	// what is still downloading or held. Drawn in divs, not SVG, so it is
	// fluid at every width: ninety days on a phone are 2px columns.
	let {
		days,
		height = 128,
		legend = true,
	}: {
		days: StatsDay[];
		height?: number;
		legend?: boolean;
	} = $props();

	let max = $derived(Math.max(1, ...days.map((d) => Math.max(d.grabbed, d.imported + d.failed))));
	let ticks = $derived(
		days.length > 2 ? [days.at(0)!, days.at(Math.floor((days.length - 1) / 2))!, days.at(-1)!] : days,
	);
	let summary = $derived(
		i18n.stats_daily_summary({
			grabbed: fmtCount(sumDays(days, "grabbed")),
			imported: fmtCount(sumDays(days, "imported")),
			failed: fmtCount(sumDays(days, "failed")),
			days: days.length,
		}),
	);

	const of = (v: number, total: number) => (total > 0 ? `${Math.min(100, (v / total) * 100)}%` : "0%");
	const col = (d: StatsDay) => Math.max(d.grabbed, d.imported + d.failed);
</script>

<figure class="m-0 min-w-0">
	{#if legend}
		<div class="mb-3 flex flex-wrap gap-x-4 gap-y-1 font-mono text-[11px] text-fg-subtle" aria-hidden="true">
			<span class="flex items-center gap-1.5">
				<span class="h-2 w-2 rounded-[2px] bg-white/[0.14]"></span>{i18n.stats_grabbed()}
			</span>
			<span class="flex items-center gap-1.5">
				<span class="h-2 w-2 rounded-[2px] bg-status-available"></span>{i18n.stats_imported()}
			</span>
			<span class="flex items-center gap-1.5">
				<span class="h-2 w-2 rounded-[2px] bg-status-failed"></span>{i18n.stats_failed()}
			</span>
		</div>
	{/if}

	<div
		class={cn("flex items-end", days.length > 45 ? "gap-px" : "gap-[3px]")}
		style:height="{height}px"
		role="img"
		aria-label={summary}
	>
		{#each days as d (d.day)}
			<div
				class="flex h-full min-w-0 flex-1 flex-col justify-end"
				title="{formatDateShort(d.day)} · {d.grabbed} {i18n.stats_grabbed()} · {d.imported} {i18n.stats_imported()} · {d.failed} {i18n.stats_failed()}"
			>
				<div
					class="flex w-full flex-col justify-end overflow-hidden rounded-t-[2px] bg-white/[0.14]"
					style:height={of(col(d), max)}
				>
					<div class="w-full shrink-0 bg-status-failed" style:height={of(d.failed, col(d))}></div>
					<div class="w-full shrink-0 bg-status-available" style:height={of(d.imported, col(d))}></div>
				</div>
			</div>
		{/each}
	</div>
	<div class="mt-2 flex justify-between border-t border-border pt-1.5 font-mono text-[10.5px] text-fg-subtle" aria-hidden="true">
		{#each ticks as t (t.day)}
			<span>{formatDateShort(t.day)}</span>
		{/each}
	</div>
</figure>
