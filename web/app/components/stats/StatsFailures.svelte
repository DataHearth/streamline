<script lang="ts">
	import { formatRelative } from "@lib/dates";
	import { fmtCount, reasonLabel, share, type StatsFailure } from "@lib/stats";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Why grabs failed, most frequent first, read off download records'
	// failure_reason codes. The bar is each reason's share of the failures,
	// and the last time it happened says whether it is still going on.
	let { failures }: { failures: StatsFailure[] } = $props();

	let total = $derived(failures.reduce((a, f) => a + f.count, 0));
</script>

{#if failures.length === 0}
	<p class="text-[13px] text-fg-subtle">{i18n.stats_no_failures()}</p>
{:else}
	<ul class="flex flex-col gap-3">
		{#each failures as f (f.reason)}
			<li class="min-w-0">
				<div class="flex min-w-0 items-baseline gap-3">
					<span class="min-w-0 flex-1 truncate text-[13px] text-fg-muted">
						{reasonLabel(f.reason)}
					</span>
					<span class="shrink-0 font-mono text-[11px] text-fg-subtle">{formatRelative(f.last_at)}</span>
					<span class="w-8 shrink-0 text-right font-mono text-[12px] tabular-nums text-status-failed">{fmtCount(f.count)}</span>
				</div>
				<div class="mt-1.5 h-[3px] overflow-hidden rounded-full bg-white/[0.06]" aria-hidden="true">
					<div class="h-full rounded-full bg-status-failed/70" style:width="{share(f.count, total)}%"></div>
				</div>
			</li>
		{/each}
	</ul>
{/if}
