<script lang="ts">
	import { cn } from "@lib/cn";
	import { auth } from "@lib/auth.svelte";
	import type { LookupItem } from "@lib/search-model.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The way out of the library on all three search surfaces, pinned outside
	// their scrolling lists so a full library cannot push it out of sight. One
	// line at any width: it sheds the question, then the chip icons, as its own
	// box narrows, and only then truncates the query.
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
	<div class={cn("@container", klass)}>
		<div class="flex items-center gap-2">
			<p class="min-w-0 truncate text-xs text-fg-subtle md:text-[13px] lg:text-xs">
				<span class="hidden @xl:inline">{i18n.search_lookup_section()}</span>
				{auth.canAddDirectly
					? i18n.search_lookup_add_as({ query })
					: i18n.search_lookup_request_as({ query })}
			</p>
			<div class="flex flex-none gap-1.5">
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
							"inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition md:text-[13px] lg:text-xs",
							i === active
								? "border-accent-line bg-accent-soft text-fg"
								: "border-border-strong bg-surface text-fg-muted hover:bg-surface-2 hover:text-fg",
						)}
					>
						<Icon size={14} class="hidden @md:block" aria-hidden="true" />
						{item.chip}
					</button>
				{/each}
			</div>
		</div>
	</div>
{/if}
