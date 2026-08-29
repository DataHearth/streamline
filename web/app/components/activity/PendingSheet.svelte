<script lang="ts">
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X } from "@lucide/svelte";
	import PendingRow from "../pending/PendingRow.svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import type { PendingItem } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
	}: {
		open: boolean;
		items: PendingItem[];
		busyId?: number | null;
		error?: boolean;
		onClose: () => void;
		onImport: (id: number) => void;
		onReplace: (id: number, removeOld: boolean) => void;
		onIgnore: (id: number, removeTorrent: boolean) => void;
	} = $props();

	$effect(() => {
		if (!open) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.common_needs_attention()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="flex items-center gap-2 text-[17px] font-semibold tracking-tight text-fg">
					Needs attention
					{#if items.length > 0}
						<span
							class="rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[11px] tabular-nums text-status-wanted"
						>
							{items.length}
						</span>
					{/if}
				</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-[max(env(safe-area-inset-bottom),18px)] pt-1"
			>
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
							/>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
