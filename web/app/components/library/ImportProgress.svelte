<script lang="ts">
	import { Loader2 } from "@lucide/svelte";
	import type { ImportScan } from "../../lib/types";

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

<div class="flex flex-col items-center gap-5 text-center">
	<div class="relative flex h-16 w-16 items-center justify-center">
		<Loader2 size={48} class="animate-spin text-accent" aria-hidden="true" />
	</div>
	<div class="space-y-1">
		<p class="text-base font-semibold text-fg">
			{scan.status === "committing" ? "Committing decisions…" : "Scanning for media files…"}
		</p>
		<p class="text-sm text-fg-muted">
			{#if scan.total_count > 0}
				{scan.processed_count} / {scan.total_count} processed
			{:else}
				Walking the directory tree…
			{/if}
		</p>
	</div>
	{#if scan.total_count > 0}
		<div
			class="w-full max-w-md"
			role="progressbar"
			aria-valuemin="0"
			aria-valuemax="100"
			aria-valuenow={pct}
		>
			<div class="h-1.5 w-full overflow-hidden rounded-full bg-bg-card">
				<div
					class="h-full rounded-full bg-accent transition-all duration-500"
					style:width="{pct}%"
				></div>
			</div>
		</div>
	{/if}
</div>
