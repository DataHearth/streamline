<script lang="ts">
	import type { Snippet } from "svelte";
	import { Check } from "@lucide/svelte";
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
		// Destructive confirms use the danger tone (red box when checked).
		tone?: "accent" | "danger";
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
		tone = "accent",
		children,
	}: Props = $props();
</script>

<label
	class={cn(
		"flex cursor-pointer items-start gap-2.5",
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
		class="peer sr-only"
	/>
	<!-- Custom box: the native accent-color glyph renders off-center at 16px,
	     so draw the check ourselves (same visual as the toolbar filter menu). -->
	<span
		aria-hidden="true"
		class={cn(
			"mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded border transition",
			"peer-focus-visible:ring-2 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-bg-card",
			checked
				? tone === "danger"
					? "border-status-failed bg-status-failed text-bg-deep"
					: "border-accent bg-accent text-fg-on-accent"
				: "border-border-strong bg-bg",
			tone === "danger"
				? "peer-focus-visible:ring-status-failed/50"
				: "peer-focus-visible:ring-accent-ring",
		)}
	>
		{#if checked}<Check size={11} strokeWidth={3} />{/if}
	</span>
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
