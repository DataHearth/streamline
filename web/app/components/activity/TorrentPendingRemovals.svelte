<script lang="ts">
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { TriangleAlert } from "@lucide/svelte";
	import type { TorrentPendingRemoval } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { removals }: { removals: TorrentPendingRemoval[] } = $props();

	let expanded = $state(false);
</script>

{#if removals.length > 0}
	<div
		role="status"
		class="mt-2 mb-3 rounded-md border border-status-wanted/25 bg-status-wanted/[0.08] px-3 text-status-wanted"
	>
		<div class="flex items-center gap-2.5">
			<TriangleAlert size={14} class="shrink-0" aria-hidden="true" />
			<p class="py-2.5 text-xs font-medium">
				{(removals.length === 1
					? i18n.torrent_pending_removals_one
					: i18n.torrent_pending_removals_other)({ count: removals.length })}
			</p>
			<button
				type="button"
				aria-expanded={expanded}
				onclick={() => (expanded = !expanded)}
				class="ml-auto min-h-11 shrink-0 rounded px-1.5 text-[11px] text-status-wanted/75 underline-offset-2 transition hover:text-status-wanted hover:underline lg:min-h-0 lg:py-2"
			>
				{expanded ? i18n.common_hide() : i18n.common_details()}
			</button>
		</div>
		{#if expanded}
			<div transition:slide={{ duration: 180, easing: cubicOut }} class="pb-3">
				<p class="text-[11.5px] leading-relaxed text-status-wanted/80">
					{i18n.torrent_pending_removals_help()}
				</p>
				<ul class="mt-2 space-y-2">
					{#each removals as removal (removal.hash)}
						<li class="text-[11.5px] leading-relaxed">
							<p class="font-medium break-words text-fg">{removal.name}</p>
							<p class="font-mono break-all text-fg-muted">{removal.path}</p>
							<p class="text-status-wanted/80">
								{removal.error
									? i18n.torrent_pending_removal_error({ error: removal.error })
									: i18n.torrent_pending_removal_waiting()}
							</p>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</div>
{/if}
