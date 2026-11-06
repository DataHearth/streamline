<script lang="ts">
	import { fmtCount, fmtPct, share, type Stats } from "@lib/stats";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Search → grab → download → import, each bar against the searches, each
	// figure with the share of the step before it. The hues are the statuses an
	// item passes through on the way (grabbing, downloading, available), so the
	// funnel reads in the colours the queue already uses.
	let { funnel }: { funnel: Stats["pipeline"]["funnel"] } = $props();

	let steps = $derived([
		{ key: "searched", label: i18n.stats_searched(), value: funnel.searched, color: "color-mix(in srgb, var(--fg) 24%, transparent)" },
		{ key: "grabbed", label: i18n.stats_grabbed(), value: funnel.grabbed, color: "var(--status-grabbing)" },
		{ key: "downloaded", label: i18n.stats_downloaded(), value: funnel.downloaded, color: "var(--status-downloading)" },
		{ key: "imported", label: i18n.stats_imported(), value: funnel.imported, color: "var(--status-available)" },
	]);
</script>

<ol class="flex flex-col gap-3">
	{#each steps as s, i (s.key)}
		<li class="grid grid-cols-[84px_minmax(0,1fr)_auto] items-center gap-3">
			<span class="truncate text-[13px] text-fg-muted">{s.label}</span>
			<div class="h-4 overflow-hidden rounded-[3px] bg-white/[0.04]" aria-hidden="true">
				<div class="h-full rounded-[3px]" style:width="{share(s.value, funnel.searched)}%" style:background={s.color}></div>
			</div>
			<span class="flex w-[96px] items-baseline justify-end gap-2 font-mono tabular-nums">
				<span class="text-[12.5px] text-fg">{fmtCount(s.value)}</span>
				<span class="w-9 text-right text-[11px] text-fg-subtle">
					{i > 0 ? fmtPct(s.value, steps[i - 1]?.value ?? 0) : ""}
				</span>
			</span>
		</li>
	{/each}
</ol>
