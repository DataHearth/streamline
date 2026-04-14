<script lang="ts" generics="T extends string">
	import { cn } from "../../lib/cn";

	type Option = { value: T; label: string; description?: string };

	type Props = {
		value: T;
		onChange: (v: T) => void;
		options: Option[];
		name?: string;
		legend?: string;
		columns?: 2 | 3;
		disabled?: boolean;
	};

	let {
		value,
		onChange,
		options,
		name,
		legend,
		columns = 2,
		disabled = false,
	}: Props = $props();

	const COLS = { 2: "sm:grid-cols-2", 3: "sm:grid-cols-3" } as const;
</script>

<fieldset {disabled}>
	{#if legend}
		<legend class="mb-2 text-sm font-medium text-fg-muted">{legend}</legend>
	{/if}
	<div class={cn("grid gap-2.5", COLS[columns])}>
		{#each options as o (o.value)}
			<label
				class="relative flex cursor-pointer flex-col gap-1.5 rounded-md border border-border bg-bg-elevated p-4 transition hover:border-border-strong has-[:checked]:border-accent has-[:checked]:bg-accent-soft has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-accent"
			>
				<input
					type="radio"
					{name}
					value={o.value}
					checked={value === o.value}
					onchange={() => onChange(o.value)}
					class="sr-only"
				/>
				<span class="text-sm font-semibold text-fg">{o.label}</span>
				{#if o.description}
					<span class="text-xs text-fg-muted">{o.description}</span>
				{/if}
			</label>
		{/each}
	</div>
</fieldset>
