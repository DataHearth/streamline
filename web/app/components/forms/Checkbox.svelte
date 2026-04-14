<script lang="ts">
	import type { Snippet } from "svelte";
	import { cn } from "../../lib/cn";

	type Props = {
		checked: boolean;
		onChange: (v: boolean) => void;
		// Simple case: a title, optionally with a secondary description line.
		label?: string;
		description?: string;
		disabled?: boolean;
		name?: string;
		class?: string;
		// Rich label content (e.g. text with inline expressions); wins over label.
		children?: Snippet;
	};

	let {
		checked,
		onChange,
		label,
		description,
		disabled = false,
		name,
		class: className,
		children,
	}: Props = $props();
</script>

<label
	class={cn(
		"flex items-start gap-2.5",
		disabled && "cursor-not-allowed opacity-60",
		className,
	)}
>
	<input
		type="checkbox"
		{name}
		{checked}
		{disabled}
		onchange={(e) => onChange((e.currentTarget as HTMLInputElement).checked)}
		class="mt-0.5 h-4 w-4 shrink-0 rounded border-border bg-bg accent-accent"
	/>
	{#if children}
		{@render children()}
	{:else if description}
		<span class="flex-1">
			<span class="block text-sm font-medium text-fg">{label}</span>
			<span class="mt-0.5 block text-xs text-fg-muted">{description}</span>
		</span>
	{:else if label}
		<span class="text-sm text-fg">{label}</span>
	{/if}
</label>
