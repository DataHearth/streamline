<script lang="ts">
	import { Globe, Info } from "@lucide/svelte";

	let { trackers }: { trackers: string[] } = $props();
</script>

{#if trackers.length === 0}
	<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
		<Globe size={24} class="text-fg-faint" aria-hidden="true" />
		<p class="text-sm font-medium text-fg">No trackers</p>
		<p class="text-xs text-fg-muted">
			This torrent relies on DHT / peer exchange only.
		</p>
	</div>
{:else}
	<ul class="flex flex-col gap-1.5">
		{#each trackers as url, i (url + i)}
			<li
				class="flex items-start gap-2.5 rounded-md border border-border bg-bg-card px-3 py-2.5"
			>
				<Globe
					size={14}
					class="mt-0.5 shrink-0 text-fg-faint"
					aria-hidden="true"
				/>
				<span class="min-w-0 break-all font-mono text-xs text-fg-muted">
					{url}
				</span>
			</li>
		{/each}
	</ul>
	<div class="mt-3 flex items-start gap-1.5 text-[11px] text-fg-faint">
		<Info size={12} class="mt-0.5 shrink-0" aria-hidden="true" />
		<span>
			The built-in engine reports announce URLs only — no per-tracker
			seeder / leecher counts.
		</span>
	</div>
{/if}
