<script lang="ts">
	import { ChevronDown } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import DropdownOption from "@components/shared/DropdownOption.svelte";

	// A field-shaped select on the detail heroes: what an artist, a book or a
	// series follows, and a series' edition.
	let {
		value,
		options,
		onChange,
		label,
		disabled = false,
		class: klass = "",
	}: {
		value: string;
		options: { key: string; label: string; hint?: string }[];
		onChange: (v: string) => void;
		label: string;
		disabled?: boolean;
		class?: string;
	} = $props();

	let open = $state(false);
	let anchor = $state<HTMLButtonElement | null>(null);
	let current = $derived(options.find((o) => o.key === value)?.label ?? "");
</script>

<button
	bind:this={anchor}
	type="button"
	{disabled}
	aria-haspopup="listbox"
	aria-expanded={open}
	aria-label="{label}: {current}"
	onclick={() => (open = !open)}
	class={cn(
		"inline-flex h-11 min-w-0 items-center justify-between gap-3 rounded-lg border border-border bg-bg-elevated/80 px-3.5 text-left text-[14px] text-fg backdrop-blur-sm transition hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60",
		klass,
	)}
>
	<span class="truncate">{current}</span>
	<ChevronDown size={16} class="shrink-0 text-fg-subtle" aria-hidden="true" />
</button>
<DropdownMenu {open} {anchor} onClose={() => (open = false)} matchAnchor ariaLabel={label}>
	{#each options as o (o.key)}
		<DropdownOption
			label={o.label}
			hint={o.hint}
			selected={o.key === value}
			onSelect={() => {
				open = false;
				if (o.key !== value) onChange(o.key);
			}}
		/>
	{/each}
</DropdownMenu>
