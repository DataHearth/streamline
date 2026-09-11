<script lang="ts">
	import { Check, type LucideIcon } from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	// One row of the one dropdown surface: a label, and any of a leading status
	// dot, a trailing count, a muted second line, and the check that marks the
	// current value. 44px tall below lg, per the touch floor.
	type Props = {
		label: string;
		onSelect: () => void;
		selected?: boolean;
		// Renders as a muted second line — a custom format's description, say.
		hint?: string;
		// A facet's population. Zero mutes the whole row: the option is offered,
		// but there is nothing behind it.
		count?: number;
		// Token class for the leading dot, e.g. "bg-status-wanted".
		dot?: string;
		// A leading glyph instead of the dot, for facets whose values are kinds
		// rather than states — a screen for a standard show, an eye for
		// monitored. Ignored when `dot` is set: one leading mark per row.
		icon?: LucideIcon;
		// Series types keep the mono lowercase they are drawn in elsewhere.
		mono?: boolean;
		title?: string;
	};

	let {
		label,
		onSelect,
		selected = false,
		hint,
		count,
		dot,
		icon: Icon,
		mono = false,
		title,
	}: Props = $props();
</script>

<li>
	<button
		type="button"
		role="option"
		aria-selected={selected}
		{title}
		onclick={onSelect}
		class={cn(
			"flex min-h-11 w-full items-start gap-2.5 px-3 py-1.5 text-left text-sm transition-colors hover:bg-bg-hover focus:outline-none focus-visible:bg-bg-hover lg:min-h-0",
			selected ? "text-accent" : count === 0 ? "text-fg-faint" : "text-fg",
		)}
	>
		{#if dot}
			<span
				class={cn("mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full", dot)}
				aria-hidden="true"
			></span>
		{:else if Icon}
			<Icon
				class={cn(
					"mt-[3px] h-3.5 w-3.5 shrink-0",
					selected ? "text-accent" : "text-fg-subtle",
				)}
				aria-hidden="true"
			/>
		{/if}
		<span class="min-w-0 flex-1">
			<span class={cn("block truncate", mono && "font-mono lowercase")}>
				{label}
			</span>
			{#if hint}
				<span
					class="mt-0.5 line-clamp-2 block text-xs font-normal text-fg-subtle"
				>
					{hint}
				</span>
			{/if}
		</span>
		{#if count != null}
			<span
				class={cn(
					"mt-[3px] shrink-0 font-mono text-[10.5px] tabular",
					selected ? "text-accent/70" : "text-fg-faint",
				)}
			>
				{count}
			</span>
		{/if}
		{#if selected}
			<Check size={14} class="mt-0.5 shrink-0 text-accent" aria-hidden="true" />
		{/if}
	</button>
</li>
