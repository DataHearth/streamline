<script lang="ts" module>
	import type { LucideIcon } from "@lucide/svelte";

	export type TouchAction = {
		key: string;
		label: string;
		icon: LucideIcon;
		danger?: boolean;
		disabled?: boolean;
		onSelect: () => void;
	};
	export type TouchMenuRow = TouchAction & {
		// One line of consequence: what the action will do to this set.
		line?: string;
		dividerBefore?: boolean;
	};
</script>

<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Ellipsis } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { NOUN_TITLE, type Noun } from "@lib/nouns";
	import { bulkMode } from "@lib/bulk-mode.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The phone half of bulk actions. It takes the bottom bar's place instead of
	// stacking above it, and everything rare or destructive goes in the sheet
	// with a line saying what it will do — a row of unlabelled icons is not
	// where a delete belongs. Counting and select-all/clear stay with
	// SelectionTopBar rather than being repeated down here.
	let {
		count,
		noun = NOUN_TITLE,
		busy = false,
		actions,
		menu,
	}: {
		count: number;
		noun?: Noun;
		busy?: boolean;
		// Three at most: the bar keeps a last cell for More.
		actions: TouchAction[];
		menu: TouchMenuRow[];
	} = $props();

	let sheet = $state(false);
	let cells = $derived(Math.min(actions.length, 3) + 1);

	// While this bar is mounted the bottom nav stands down.
	$effect(() => {
		bulkMode.set(true);
		return () => bulkMode.set(false);
	});

	function run(row: TouchAction) {
		sheet = false;
		row.onSelect();
	}

	const cell =
		"flex flex-col items-center justify-center gap-1.5 px-2 pb-3 pt-2.5 text-[11px] font-medium transition disabled:opacity-40";
</script>

<div
	role="toolbar"
	aria-label={i18n.bulk_actions()}
	aria-busy={busy}
	transition:fly={{ y: 64, duration: 200, easing: cubicOut }}
	class={twMerge(
		"fixed inset-x-0 bottom-0 z-40 grid border-t border-border-strong bg-bg-elevated/96 pb-[env(safe-area-inset-bottom)] backdrop-blur-md md:hidden",
		busy && "pointer-events-none opacity-60",
	)}
	style:grid-template-columns="repeat({cells}, minmax(0, 1fr))"
>
	{#each actions.slice(0, 3) as a (a.key)}
		<button
			type="button"
			disabled={busy || a.disabled}
			onclick={() => run(a)}
			class={twMerge(cell, a.danger ? "text-status-failed" : "text-fg-muted")}
		>
			<a.icon size={22} aria-hidden="true" />
			{a.label}
		</button>
	{/each}
	<button
		type="button"
		disabled={busy}
		onclick={() => (sheet = true)}
		aria-haspopup="dialog"
		aria-expanded={sheet}
		class={twMerge(cell, "text-fg-muted")}
	>
		<Ellipsis size={22} aria-hidden="true" />
		{i18n.common_more()}
	</button>
</div>

{#if sheet}
	<BottomSheet
		title={noun.count(count)}
		label={i18n.bulk_actions()}
		onClose={() => (sheet = false)}
		hideFrom="md"
		bodyClass="px-2.5 pb-[max(env(safe-area-inset-bottom),12px)]"
	>
		{#each menu as row (row.key)}
			{#if row.dividerBefore}
				<div class="my-2 h-px bg-border" role="presentation"></div>
			{/if}
			<button
				type="button"
				disabled={busy || row.disabled}
				onclick={() => run(row)}
				class={twMerge(
					"flex w-full items-center gap-3.5 rounded-xl px-2.5 py-3 text-left transition active:bg-surface disabled:opacity-40",
					row.danger ? "text-status-failed" : "text-fg",
				)}
			>
				<row.icon size={22} class="shrink-0" aria-hidden="true" />
				<span class="min-w-0 flex-1">
					<span class="block text-[15px] font-medium leading-tight tracking-tight">
						{row.label}
					</span>
					{#if row.line}
						<span class="mt-1 block font-mono text-[11px] text-fg-subtle">
							{row.line}
						</span>
					{/if}
				</span>
			</button>
		{/each}
	</BottomSheet>
{/if}
