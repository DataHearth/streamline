<script lang="ts">
	import { fmtCount, fmtPct, share, type StatsIndexer } from "@lib/stats";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Which indexers the grabs came from and how they turned out, busiest
	// first. The failure rate turns the warning hue past 10% and failed past
	// 25%: an indexer that keeps handing out dead torrents is the one to look at.
	let { indexers }: { indexers: StatsIndexer[] } = $props();

	let grabs = $derived(indexers.reduce((a, x) => a + x.grabs, 0));
	const rateTone = (x: StatsIndexer) => {
		const r = x.grabs > 0 ? x.failed / x.grabs : 0;
		return r >= 0.25 ? "text-status-failed" : r >= 0.1 ? "text-status-warning" : "text-fg-muted";
	};
	const head = "font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint";
</script>

<div class="min-w-0">
	<div class="idx-grid hidden border-b border-border px-4 pb-2 md:grid md:px-5" aria-hidden="true">
		<span class={head}>{i18n.settings_indexers()}</span>
		<span class={cn(head, "text-right")}>{i18n.stats_col_grabs()}</span>
		<span class={cn(head, "text-right")}>{i18n.stats_imported()}</span>
		<span class={cn(head, "whitespace-nowrap text-right")}>{i18n.stats_col_fail_rate()}</span>
		<span class={head}>{i18n.stats_col_share()}</span>
	</div>
	<ul>
		{#each indexers as x, i (x.name)}
			<li class={cn("px-4 py-2.5 md:px-5", i > 0 && "border-t border-border")}>
				<!-- Phone -->
				<div class="md:hidden">
					<div class="flex items-baseline gap-3">
						<span class="min-w-0 flex-1 truncate text-[13.5px] font-medium text-fg">{x.name}</span>
						<span class="font-mono text-[12px] tabular-nums text-fg">{fmtCount(x.grabs)}</span>
					</div>
					<div class="mt-1 flex gap-3 font-mono text-[11px] text-fg-subtle">
						<span><span class="tabular-nums text-fg-muted">{fmtCount(x.imported)}</span> {i18n.stats_imported_lc()}</span>
						<span><span class={cn("tabular-nums", rateTone(x))}>{fmtPct(x.failed, x.grabs)}</span> {i18n.stats_failed_lc()}</span>
					</div>
				</div>
				<!-- md and up -->
				<div class="idx-grid hidden items-center md:grid">
					<span class="min-w-0 truncate text-[13px] font-medium text-fg">{x.name}</span>
					<span class="text-right font-mono text-[12.5px] tabular-nums text-fg">{fmtCount(x.grabs)}</span>
					<span class="text-right font-mono text-[12.5px] tabular-nums text-fg-muted">{fmtCount(x.imported)}</span>
					<span class={cn("text-right font-mono text-[12.5px] tabular-nums", rateTone(x))}>{fmtPct(x.failed, x.grabs)}</span>
					<span class="h-[3px] overflow-hidden rounded-full bg-white/[0.06]" aria-hidden="true">
						<span class="block h-full rounded-full bg-status-grabbing" style:width="{share(x.grabs, grabs)}%"></span>
					</span>
				</div>
			</li>
		{/each}
	</ul>
</div>

<style>
	.idx-grid {
		grid-template-columns: minmax(0, 1.4fr) 56px 64px 100px minmax(0, 1fr);
		column-gap: 12px;
	}
</style>
