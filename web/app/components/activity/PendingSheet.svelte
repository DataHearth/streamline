<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import PendingRow from "@components/pending/PendingRow.svelte";
	import type { PendingItem } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Below md the "needs attention" section is one banner line on the page; the
	// proposals themselves open here, where each one gets the full width its three
	// decisions need.
	let {
		open,
		items,
		busyId = null,
		error = false,
		onClose,
		onImport,
		onReplace,
		onIgnore,
		onForget,
	}: {
		open: boolean;
		items: PendingItem[];
		busyId?: number | null;
		error?: boolean;
		onClose: () => void;
		onImport: (id: number) => void;
		onReplace: (id: number, removeOld: boolean) => void;
		onIgnore: (id: number, removeTorrent: boolean) => void;
		onForget: (id: number) => void;
	} = $props();

</script>

{#if open}
	<BottomSheet
		label={i18n.common_needs_attention()}
		{onClose}
		hideFrom="md"
		bodyClass="pb-[max(env(safe-area-inset-bottom),18px)] pt-1"
	>
		{#snippet header()}
			<h2 class="flex items-center gap-2 text-[17px] font-semibold tracking-tight text-fg">
				{i18n.common_needs_attention()}
				{#if items.length > 0}
					<span
						class="rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[11px] tabular-nums text-status-wanted"
					>
						{items.length}
					</span>
				{/if}
			</h2>
		{/snippet}
		{#if error}
			<p class="py-6 text-center text-sm text-status-failed">
				{i18n.torrent_proposals_failed()}
			</p>
		{:else if items.length === 0}
			<p class="py-6 text-center text-sm text-fg-muted">
				{i18n.activity_nothing_waiting()}
			</p>
		{:else}
			<div class="flex flex-col gap-2.5">
				{#each items as item (item.id)}
					<PendingRow
						{item}
						busy={busyId === item.id}
						onImport={() => onImport(item.id)}
						onReplace={(removeOld) => onReplace(item.id, removeOld)}
						onIgnore={(removeTorrent) => onIgnore(item.id, removeTorrent)}
						onForget={() => onForget(item.id)}
					/>
				{/each}
			</div>
		{/if}
	</BottomSheet>
{/if}
