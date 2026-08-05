<script lang="ts">
	import { ListChecks, CheckCheck } from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	let {
		active,
		count,
		total,
		onActiveChange,
		onSelectAll,
	}: {
		active: boolean;
		count: number;
		// Rows currently visible — "select all" follows the active filters, so an
		// empty result has nothing to offer.
		total: number;
		onActiveChange: (v: boolean) => void;
		onSelectAll: () => void;
	} = $props();

	const base =
		"inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border px-3 text-[12.5px] font-medium transition";
	const off =
		"border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg";
	const on = "border-accent bg-accent-soft text-accent-text";
</script>

<button
	type="button"
	onclick={() => onActiveChange(!active)}
	aria-pressed={active}
	class={cn(base, active ? on : off)}
>
	<ListChecks size={14} aria-hidden="true" />
	Select
	{#if count > 0}
		<span
			class={cn(
				"rounded-sm bg-white/[0.04] px-1.5 py-px font-mono text-[10px] tabular",
				active ? "text-accent-text" : "text-fg-faint",
			)}
		>
			{count}
		</span>
	{/if}
</button>

<button
	type="button"
	onclick={onSelectAll}
	disabled={total === 0}
	class={cn(base, off, "hidden lg:inline-flex", "disabled:pointer-events-none disabled:opacity-40")}
>
	<CheckCheck size={14} aria-hidden="true" />
	Select all
</button>
