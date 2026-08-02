<script lang="ts">
	import { Check, Minus } from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	let {
		checked,
		indeterminate = false,
		onChange,
		label,
		variant = "row",
	}: {
		checked: boolean;
		indeterminate?: boolean;
		onChange: (v: boolean) => void;
		label: string;
		// "card" floats over a poster (needs its own contrast); "row" sits in a
		// table cell and matches the form checkbox.
		variant?: "row" | "card";
	} = $props();

	function toggle(e: MouseEvent) {
		e.preventDefault();
		e.stopPropagation();
		onChange(!checked);
	}
</script>

<button
	type="button"
	role="checkbox"
	aria-checked={indeterminate ? "mixed" : checked}
	aria-label={label}
	title={label}
	onclick={toggle}
	class={cn(
		"grid shrink-0 place-items-center transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
		variant === "card"
			? "h-6 w-6 rounded-md border backdrop-blur-sm"
			: "h-[18px] w-[18px] rounded border",
		checked || indeterminate
			? "border-accent bg-accent text-fg-on-accent"
			: variant === "card"
				? "border-white/25 bg-black/65 text-transparent hover:border-white/50"
				: "border-border-strong bg-bg text-transparent hover:border-fg-subtle",
	)}
>
	{#if indeterminate}
		<Minus size={variant === "card" ? 14 : 12} strokeWidth={3} aria-hidden="true" />
	{:else if checked}
		<Check size={variant === "card" ? 14 : 12} strokeWidth={3} aria-hidden="true" />
	{/if}
</button>
