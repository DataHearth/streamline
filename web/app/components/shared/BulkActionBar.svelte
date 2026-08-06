<script lang="ts">
	import type { Snippet } from "svelte";
	import { X } from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { plural } from "../../lib/bulk";

	let {
		count,
		total,
		noun = "title",
		nounPlural,
		busy = false,
		onSelectAll,
		onClear,
		children,
	}: {
		count: number;
		total: number;
		noun?: string;
		// Nouns whose plural isn't noun + "s" — "series" pluralises to itself.
		nounPlural?: string;
		busy?: boolean;
		onSelectAll: () => void;
		onClear: () => void;
		children: Snippet;
	} = $props();
</script>

<!-- md and up only: below md the phone touch bar (BulkTouchBar) takes the bottom
     bar's place instead. From md the rail owns navigation, so this can sit at
     the bottom of the viewport. -->
<div
	role="toolbar"
	aria-label="Bulk actions"
	transition:fly={{ duration: 180, y: 12, easing: cubicOut }}
	class="pointer-events-none fixed inset-x-0 bottom-6 z-40 flex justify-center px-4"
>
	<div
		class="pointer-events-auto flex max-w-full items-center gap-2 overflow-x-auto rounded-lg border border-border-strong bg-bg-elevated/95 p-2 shadow-4 backdrop-blur-md [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
	>
		<div class="flex shrink-0 items-center gap-2.5 pl-1 pr-1">
			<span class="whitespace-nowrap text-[13px] font-semibold text-fg">
				{plural(count, noun, nounPlural)}
			</span>
			{#if count < total}
				<button
					type="button"
					onclick={onSelectAll}
					class="whitespace-nowrap font-mono text-[11px] text-accent-text underline-offset-2 transition hover:underline"
				>
					select all {total}
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

		<button
			type="button"
			onclick={onClear}
			aria-label="Clear selection"
			title="Clear selection"
			class="grid h-9 w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			<X size={16} aria-hidden="true" />
		</button>
	</div>
</div>
