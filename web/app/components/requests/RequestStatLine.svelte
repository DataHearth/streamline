<script lang="ts">
	import type { RequestCounts } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// D2: the four figures as one hairline-split line below md, the shape the
	// activity page uses when its tiles don't fit. The tiles return from md up.
	let { counts }: { counts: RequestCounts } = $props();

	// Each figure carries its own status colour, the same tokens the rows and the
	// pills use, so the line reads as a legend for the list under it. A zero has
	// nothing to announce and stays muted.
	let cells = $derived([
		{ k: "pending", n: counts.pending, l: i18n.lc_pending(), cls: "text-status-wanted" },
		{ k: "approved", n: counts.approved, l: i18n.lc_approved(), cls: "text-status-grabbing" },
		{ k: "rejected", n: counts.denied, l: i18n.lc_rejected(), cls: "text-status-failed" },
		{ k: "available", n: counts.available, l: i18n.lc_available(), cls: "text-status-available" },
	]);
</script>

<div
	class="mb-3.5 flex h-11 items-center rounded-lg border border-border bg-bg-elevated md:hidden"
>
	{#each cells as c (c.k)}
		<div
			class="flex flex-1 items-baseline justify-center gap-1.5 border-l border-border text-[11.5px] text-fg-subtle first:border-l-0"
		>
			<span
				class="font-mono text-[14px] font-semibold tabular-nums {c.n > 0
					? c.cls
					: 'text-fg-faint'}"
			>
				{c.n}
			</span>
			{c.l}
		</div>
	{/each}
</div>
