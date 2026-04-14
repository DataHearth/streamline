<script lang="ts" generics="T extends string">
	import type { Snippet } from "svelte";
	import { cn } from "../../lib/cn";

	type Option = { value: T; label: string };

	type Props = {
		label: string;
		value: T;
		onChange: (v: T) => void;
		options: Option[];
		name?: string;
		// When set (edit mode), only the active option stays selectable.
		locked?: boolean;
		lockedHint?: string;
		// Renders the brand icon for a given option value.
		logo: Snippet<[T]>;
	};

	let {
		label,
		value,
		onChange,
		options,
		name,
		locked = false,
		lockedHint,
		logo,
	}: Props = $props();
</script>

<div
	class="flex flex-wrap items-center gap-2"
	role="radiogroup"
	aria-label={label}
>
	<span
		class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
		>{label}</span
	>
	{#each options as o (o.value)}
		{@const active = value === o.value}
		{@const disabled = locked && !active}
		<label
			title={o.label}
			class={cn(
				"grid h-9 w-9 place-items-center rounded-md border border-border bg-bg-card text-fg-muted transition hover:border-border-strong has-[:checked]:border-accent has-[:checked]:bg-accent-soft has-[:checked]:text-fg has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-accent",
				locked ? "cursor-not-allowed" : "cursor-pointer",
				disabled && "opacity-40",
			)}
		>
			<input
				type="radio"
				{name}
				value={o.value}
				checked={active}
				{disabled}
				onchange={() => onChange(o.value)}
				class="sr-only"
			/>
			{@render logo(o.value)}
		</label>
	{/each}
	{#if locked && lockedHint}
		<span class="text-[11px] text-fg-subtle">{lockedHint}</span>
	{/if}
</div>
