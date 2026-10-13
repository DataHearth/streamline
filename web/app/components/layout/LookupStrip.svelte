<script lang="ts">
	import { cn } from "@lib/cn";
	import { auth } from "@lib/auth.svelte";
	import type { LookupItem } from "@lib/search-model.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The way out of the library on all three search surfaces, pinned outside
	// their scrolling lists so a full library cannot push it out of sight.
	let {
		items,
		onpick,
		active = -1,
		onhover,
		class: klass = "",
	}: {
		items: LookupItem[];
		onpick: (item: LookupItem) => void;
		// The palette's keyboard cursor, as an index into `items`.
		active?: number;
		onhover?: (index: number) => void;
		class?: string;
	} = $props();

	let query = $derived(items[0]?.query ?? "");
</script>

{#if items.length > 0}
	<div class={cn("flex flex-col gap-2 px-3 py-2.5", klass)}>
		<p class="truncate text-[12px] text-fg-subtle">
			{i18n.search_lookup_section()}
			{auth.canAddDirectly
				? i18n.search_lookup_add_as({ query })
				: i18n.search_lookup_request_as({ query })}
		</p>
		<div class="flex flex-wrap gap-1.5">
			{#each items as item, i (item.media)}
				{@const Icon = item.icon}
				<button
					type="button"
					aria-label={item.label}
					title={item.label}
					onclick={() => onpick(item)}
					onmouseenter={() => onhover?.(i)}
					data-cmd-active={i === active ? "" : undefined}
					class={cn(
						"inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-[12.5px] font-medium transition-colors",
						i === active
							? "border-accent-line bg-accent-soft text-fg"
							: "border-border bg-surface text-fg-muted hover:text-fg",
					)}
				>
					<Icon size={14} aria-hidden="true" />
					{item.chip}
				</button>
			{/each}
		</div>
	</div>
{/if}
