<script lang="ts">
	import type { Component } from "svelte";
	import { cn } from "../../lib/cn";

	type Props = {
		checked: boolean;
		onChange: (v: boolean) => void;
		label: string;
		// "accent" for capability toggles (HTTPS/SSL), "status" for enabled state.
		tone?: "accent" | "status";
		// Optional leading icon; falls back to a filled dot when omitted.
		icon?: Component<{ size?: number; class?: string }>;
		name?: string;
		disabled?: boolean;
	};

	let {
		checked,
		onChange,
		label,
		tone = "accent",
		icon: Icon,
		name,
		disabled = false,
	}: Props = $props();

	const TONE = {
		accent:
			"text-fg-muted has-[:checked]:border-accent has-[:checked]:bg-accent-soft has-[:checked]:text-accent-text",
		status:
			"text-fg-faint has-[:checked]:border-status-available/40 has-[:checked]:bg-status-available/10 has-[:checked]:text-status-available",
	} as const;
</script>

<label
	class={cn(
		"inline-flex h-10 cursor-pointer items-center justify-center gap-1.5 rounded-md border border-border bg-bg-base px-3 text-xs font-medium transition hover:border-border-strong has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-accent",
		TONE[tone],
		disabled && "cursor-not-allowed opacity-50",
	)}
>
	<input
		type="checkbox"
		{name}
		{disabled}
		{checked}
		onchange={(e) => onChange((e.currentTarget as HTMLInputElement).checked)}
		class="sr-only"
	/>
	{#if Icon}
		<Icon size={14} class="shrink-0" />
	{:else}
		<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-current"></span>
	{/if}
	<span class="leading-none">{label}</span>
</label>
