<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import type { Snippet } from "svelte";
	import { X } from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { NOUN_TITLE, type Noun } from "@lib/nouns";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { twMerge } from "tailwind-merge";
	import { keyboardUp } from "@lib/keyboard.svelte";

	let {
		count,
		total,
		noun = NOUN_TITLE,
		busy = false,
		onSelectAll,
		onClear,
		children,
	}: {
		count: number;
		total: number;
		noun?: Noun;
		busy?: boolean;
		onSelectAll: () => void;
		onClear: () => void;
		children: Snippet;
	} = $props();
</script>

<!-- md and up only: below md the phone touch bar (BulkTouchBar) takes the bottom
     bar's place instead. From md the rail owns navigation, so this can sit at
     the bottom of the viewport. On a touch tablet the on-screen keyboard takes
     its place while it is up. -->
<div
	role="toolbar"
	aria-label={i18n.bulk_actions()}
	transition:fly={{ duration: 180, y: 12, easing: cubicOut }}
	class={twMerge(
		"pointer-events-none fixed inset-x-0 bottom-6 z-40 flex justify-center px-4",
		keyboardUp() && "hidden",
	)}
>
	<div
		class="pointer-events-auto flex max-w-full items-center gap-2 overflow-x-auto rounded-lg border border-border-strong bg-bg-elevated/95 p-2 shadow-4 backdrop-blur-md [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
	>
		<div class="flex shrink-0 items-center gap-2.5 pl-1 pr-1">
			<span class="whitespace-nowrap text-[13px] font-semibold text-fg">
				{noun.count(count)}
			</span>
			{#if count < total}
				<button
					type="button"
					onclick={onSelectAll}
					class="whitespace-nowrap font-mono text-[11px] text-accent-text underline-offset-2 transition hover:underline"
				>
					{i18n.bulk_select_all_n_lc({ total })}
				</button>
			{/if}
		</div>

		<div class="h-6 w-px shrink-0 bg-border" aria-hidden="true"></div>

		<div
			class="flex shrink-0 items-center gap-1.5"
			aria-busy={busy}
			class:opacity-60={busy}
		>
			{@render children()}
		</div>

		<div class="h-6 w-px shrink-0 bg-border" aria-hidden="true"></div>

		<Button
			variant="ghost"
			size="icon"
			class="shrink-0"
			type="button"
			onclick={onClear}
			aria-label={i18n.bulk_clear_selection()}
			title={i18n.bulk_clear_selection()}
		>
			<X size={16} aria-hidden="true" />
		</Button>
	</div>
</div>
