<script lang="ts">
	import { fmtPct, fmtSize, share, volumeTone, type StatsVolume } from "@lib/stats";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Each library root and the data directory, as Settings → General reports
	// them: the path, used of total, and what is left. A volume past 80% turns
	// the warning hue, past 90% failed, so the one that needs space shows first.
	let { volumes }: { volumes: StatsVolume[] } = $props();
</script>

<ul class="flex flex-col gap-4">
	{#each volumes as v (v.key)}
		{@const pct = share(v.used_bytes, v.total_bytes)}
		<li class="min-w-0">
			<div class="flex min-w-0 items-baseline gap-3">
				<span class="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-muted" title={v.path}>{v.path}</span>
				<span
					class="shrink-0 font-mono text-[12px] tabular-nums"
					style:color={pct >= 80 ? volumeTone(v) : "var(--fg)"}
				>
					{fmtPct(v.used_bytes, v.total_bytes)}
				</span>
			</div>
			<div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-[var(--progress-track)]" aria-hidden="true">
				<div class="h-full rounded-full" style:width="{pct}%" style:background={volumeTone(v)}></div>
			</div>
			<div class="mt-1 flex justify-between font-mono text-[11px] text-fg-subtle">
				<span>{fmtSize(v.used_bytes)} / {fmtSize(v.total_bytes)}</span>
				<span>{i18n.stats_free({ size: fmtSize(v.total_bytes - v.used_bytes) })}</span>
			</div>
		</li>
	{/each}
</ul>
