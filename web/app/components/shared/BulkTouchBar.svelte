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
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Ellipsis, X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { plural } from "../../lib/bulk";
	import { bulkMode } from "../../lib/bulk-mode.svelte";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The phone half of bulk actions. It takes the bottom bar's place instead of
	// stacking above it, and everything rare or destructive goes in the sheet
	// with a line saying what it will do — a row of unlabelled icons is not
	// where a delete belongs. Counting and select-all/clear stay with
	// SelectionTopBar rather than being repeated down here.
	let {
		count,
		noun = "title",
		nounPlural,
		busy = false,
		actions,
		menu,
	}: {
		count: number;
		noun?: string;
		nounPlural?: string;
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

	$effect(() => {
		if (!sheet) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") sheet = false;
		};
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
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
	class={cn(
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
			class={cn(cell, a.danger ? "text-status-failed" : "text-fg-muted")}
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
		class={cn(cell, "text-fg-muted")}
	>
		<Ellipsis size={22} aria-hidden="true" />
		{i18n.common_more()}
	</button>
</div>

{#if sheet}
	<div
		class="fixed inset-0 z-50 md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.bulk_actions()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={() => (sheet = false)}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: () => (sheet = false) }}
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
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">
					{plural(count, noun, nounPlural)}
				</h2>
				<button
					type="button"
					onclick={() => (sheet = false)}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2.5 pb-[max(env(safe-area-inset-bottom),12px)]"
			>
				{#each menu as row (row.key)}
					{#if row.dividerBefore}
						<div class="my-2 h-px bg-border" role="presentation"></div>
					{/if}
					<button
						type="button"
						disabled={busy || row.disabled}
						onclick={() => run(row)}
						class={cn(
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
			</div>
		</div>
	</div>
{/if}
