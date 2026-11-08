<script lang="ts">
	import { Check, Minus } from "@lucide/svelte";
	import { cn } from "@lib/cn";

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

	let on = $derived(checked || indeterminate);

	// The box shows the props, not the click: the input goes back to them and
	// the parent decides whether the new value sticks.
	function change(e: Event & { currentTarget: HTMLInputElement }) {
		const next = e.currentTarget.checked;
		e.currentTarget.checked = checked;
		e.currentTarget.indeterminate = indeterminate;
		onChange(next);
	}
</script>

<span class={cn("grid shrink-0 place-items-center", on ? "text-fg-on-accent" : "text-transparent")}>
	<!-- The rows and cards it sits on open something on click. -->
	<input
		type="checkbox"
		{checked}
		{indeterminate}
		aria-label={label}
		title={label}
		onclick={(e) => e.stopPropagation()}
		onchange={change}
		class={cn(
			"native col-start-1 row-start-1 m-0 cursor-pointer appearance-none border transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
			variant === "card"
				? "h-10 w-10 rounded-md backdrop-blur-sm lg:h-6 lg:w-6"
				: "h-[18px] w-[18px] rounded",
			on
				? "border-accent bg-accent"
				: variant === "card"
					? "border-white/25 bg-black/65 hover:border-white/50"
					: "border-border-strong bg-bg hover:border-fg-subtle",
		)}
	/>
	{#if indeterminate}
		<Minus
			size={variant === "card" ? 14 : 12}
			strokeWidth={3}
			class="pointer-events-none relative col-start-1 row-start-1"
			aria-hidden="true"
		/>
	{:else if checked}
		<Check
			size={variant === "card" ? 14 : 12}
			strokeWidth={3}
			class="pointer-events-none relative col-start-1 row-start-1"
			aria-hidden="true"
		/>
	{/if}
</span>
