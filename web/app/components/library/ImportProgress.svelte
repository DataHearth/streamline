<script lang="ts">
	import type { ImportScan } from "../../lib/types";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = { scan: ImportScan };
	let { scan }: Props = $props();

	const pct = $derived(
		scan.total_count > 0
			? Math.min(
					100,
					Math.floor((scan.processed_count * 100) / scan.total_count),
				)
			: 0,
	);
</script>

<!--
  This state already knows what it is doing and how far along it is, so it
  narrates rather than spins. The 48px LoaderCircle that used to sit above the
  bar was the second moving thing in a state that has real progress to show —
  the bar carries it now, indeterminate while the scan is still walking the
  tree and determinate once it knows the file count.
-->
<div class="flex w-full flex-col items-center gap-4 text-center" role="status" aria-live="polite">
	<div class="space-y-1">
		<p class="text-base font-semibold text-fg">
			{scan.status === "committing" ? i18n.imports_committing() : i18n.imports_scanning()}
		</p>
		<p class="text-sm text-fg-muted">
			{#if scan.total_count > 0}
				{i18n.imports_processed_count({
					done: scan.processed_count,
					total: scan.total_count,
				})}
			{:else}
				{i18n.imports_walking_tree()}
			{/if}
		</p>
	</div>
	<div class="w-full max-w-md">
		<ProgressBar
			value={scan.total_count > 0 ? pct / 100 : undefined}
			status="importing"
			height={2}
			label={i18n.imports_scanning()}
		/>
	</div>
</div>
