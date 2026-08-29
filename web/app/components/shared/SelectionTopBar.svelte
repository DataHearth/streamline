<script lang="ts">
	import { X } from "@lucide/svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Phone header for an active selection: it replaces the filter line rather
	// than sitting on top of it, so entering selection mode costs no height. The
	// count is the title, so the bar can never claim a set it doesn't have.
	let {
		count,
		total,
		noun = "title",
		nounPlural,
		onClear,
		onSelectAll,
	}: {
		count: number;
		total: number;
		noun?: string;
		nounPlural?: string;
		onClear: () => void;
		onSelectAll: () => void;
	} = $props();
</script>

<div
	class="flex items-center gap-2 border-b border-accent-line bg-accent-soft px-2.5 py-2 md:hidden"
	role="toolbar"
	aria-label={i18n.bulk_selection()}
>
	<button
		type="button"
		onclick={onClear}
		aria-label={i18n.bulk_exit_selection()}
		class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-full text-accent-text transition active:bg-white/[0.06]"
	>
		<X size={18} aria-hidden="true" />
	</button>
	<span
		class="min-w-0 flex-1 truncate text-[15px] font-semibold tracking-tight text-accent-text"
		aria-live="polite"
	>
		{count === 0 ? i18n.a11y_select_all_of({ items: nounPlural ?? noun + "s" }) : `${count} selected`}
	</span>
	{#if count < total}
		<button
			type="button"
			onclick={onSelectAll}
			class="shrink-0 rounded-full px-3 py-2 text-[13px] font-medium text-accent-text transition active:bg-white/[0.06]"
		>
			Select all {total}
		</button>
	{:else}
		<button
			type="button"
			onclick={onClear}
			class="shrink-0 rounded-full px-3 py-2 text-[13px] font-medium text-accent-text transition active:bg-white/[0.06]"
		>
			{i18n.common_clear()}
		</button>
	{/if}
</div>
