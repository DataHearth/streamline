<script lang="ts">
	import { Check } from "@lucide/svelte";
	import { formatDateShort } from "@lib/dates";
	import type { Release } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The short state at the end of a release row: a check when it is all
	// there, what is missing when it is not, how far a download has got, or the
	// day an upcoming one comes out.
	let { release: r }: { release: Release } = $props();
</script>

{#if r.status === "upcoming"}
	<span class="shrink-0 font-mono text-[10.5px] text-fg-subtle">{formatDateShort(r.release_date)}</span>
{:else if r.status === "downloading"}
	<span class="shrink-0 font-mono text-[10.5px] text-status-downloading">{Math.round(r.progress ?? 0)}%</span>
{:else if r.status === "wanted"}
	<span class="shrink-0 font-mono text-[10.5px] text-status-wanted">
		{r.tracks_have > 0 ? `${r.tracks_have}/${r.track_count}` : i18n.status_wanted()}
	</span>
{:else}
	<span class="shrink-0 text-status-available" title={i18n.status_available()}>
		<Check size={14} aria-hidden="true" />
		<span class="sr-only">{i18n.status_available()}</span>
	</span>
{/if}
