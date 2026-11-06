<script lang="ts">
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { requireAdmin } from "@lib/guards";
	import { formatRelative } from "@lib/dates";
	import { RANGE_LABEL, STATS_RANGES, type Stats, type StatsRange } from "@lib/stats";
	import { cn } from "@lib/cn";
	import StatsReport from "@components/stats/StatsReport.svelte";
	import StatsSkeleton from "@components/stats/StatsSkeleton.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Admin stats: what the library holds, how the pipeline fills it, who asks
	// for what — over the last 7, 30 or 90 days (GET /stats, lib/stats).
	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	// ?range= is honoured so a link can open a given period; the control
	// itself does not write it back.
	function initialRange(): StatsRange {
		const r = new URLSearchParams(window.location.search).get("range");
		return (STATS_RANGES as string[]).includes(r ?? "") ? (r as StatsRange) : "30d";
	}
	let range = $state<StatsRange>(initialRange());

	const qc = useQueryClient();

	// Computed on demand server-side over the event table, so it is polled
	// slowly: nothing on this page is something anyone watches change.
	const statsQuery = createQuery<Stats>(() => ({
		queryKey: ["stats", range],
		queryFn: () => api<Stats>(`/stats?range=${range}`),
		enabled: auth.isAdmin,
		refetchInterval: 300000,
	}));

	// A range change keeps the previous answer on screen, dimmed, until the
	// new one lands: the page holds its height and nothing jumps to a skeleton.
	let last = $state<Stats | null>(null);
	$effect(() => {
		if (statsQuery.data) last = statsQuery.data;
	});
	let stats = $derived(statsQuery.data ?? last);
	// The answer names its range, so "still the old one" holds whether the
	// query empties on a key change (TanStack) or keeps its data (the harness).
	let stale = $derived(!!stats && stats.range !== range);
</script>

{#snippet rangeControl()}
	<div
		role="group"
		aria-label={i18n.stats_range()}
		class="inline-flex gap-0.5 rounded-md border border-border bg-bg-elevated p-[3px]"
	>
		{#each STATS_RANGES as r (r)}
			<button
				type="button"
				aria-pressed={range === r}
				onclick={() => (range = r)}
				class={cn(
					"h-[30px] rounded-sm px-3 text-[12.5px] font-medium transition-colors",
					range === r ? "bg-accent-soft text-accent-text" : "text-fg-subtle hover:text-fg",
				)}
			>
				{RANGE_LABEL[r]()}
			</button>
		{/each}
	</div>
	<p class="hidden min-w-0 font-mono text-[11px] text-fg-subtle md:block">
		{i18n.stats_compare({ range: RANGE_LABEL[range]() })}
	</p>
{/snippet}

<div class="mx-auto w-full max-w-7xl px-4 pb-10 pt-5 md:px-8 md:pb-12 md:pt-7">
	{#if stats}
		<p class="mb-5 font-mono text-[11px] text-fg-subtle md:mb-6">
			{i18n.stats_updated({ when: formatRelative(stats.generated_at) })}
		</p>
	{/if}

	{#if statsQuery.isError && !stats}
		<div class="flex flex-col items-start gap-3 rounded-lg border border-border bg-bg-elevated p-5">
			<p class="text-[14px] text-fg">{i18n.stats_load_failed()}</p>
			<p class="text-[13px] text-fg-subtle">{errorText(statsQuery.error, i18n.err_generic())}</p>
			<button
				type="button"
				onclick={() => qc.refetchQueries({ queryKey: ["stats", range] })}
				class="h-9 rounded-md border border-border-strong bg-surface px-3.5 text-[13px] font-semibold text-fg transition hover:bg-surface-2"
			>
				{i18n.common_retry()}
			</button>
		</div>
	{:else if !stats}
		<StatsSkeleton />
	{:else}
		<div class={cn("transition-opacity duration-200", stale && "opacity-60")} aria-busy={stale}>
			<StatsReport {stats} {rangeControl} />
		</div>
	{/if}
</div>
