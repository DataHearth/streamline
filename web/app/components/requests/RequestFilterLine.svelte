<script lang="ts">
	import { Search, SlidersHorizontal, X } from "@lucide/svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// C2, with the status control removed: the segmented Pending / Decided / All
	// went into the sheet with everything else, and the line that replaces it
	// answers the question this page is actually asked — "did my thing get
	// approved" / "what did Camille ask for" — which no status filter can.
	let {
		query,
		onQueryChange,
		activeCount = 0,
		onOpenFilter,
	}: {
		query: string;
		onQueryChange: (q: string) => void;
		activeCount?: number;
		onOpenFilter: () => void;
	} = $props();
</script>

<div class="mb-3 flex items-center gap-2.5 lg:hidden">
	<div
		class="flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
	>
		<Search class="h-4 w-4 shrink-0 text-fg-subtle" aria-hidden="true" />
		<input
			type="search"
			value={query}
			oninput={(e) => onQueryChange(e.currentTarget.value)}
			placeholder={i18n.requests_search_placeholder()}
			aria-label={i18n.requests_search_aria()}
			class="min-w-0 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-fg-faint"
		/>
		{#if query}
			<button
				type="button"
				onclick={() => onQueryChange("")}
				aria-label={i18n.common_clear_search()}
				class="grid h-7 w-7 shrink-0 place-items-center rounded-full text-fg-faint transition active:bg-surface"
			>
				<X size={14} aria-hidden="true" />
			</button>
		{/if}
	</div>

	<button
		type="button"
		onclick={onOpenFilter}
		aria-label={i18n.requests_filter()}
		class="relative grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-border-strong text-fg-muted transition active:bg-surface"
	>
		<SlidersHorizontal size={17} aria-hidden="true" />
		{#if activeCount > 0}
			<span
				class="absolute -right-1 -top-1 grid h-[17px] min-w-[17px] place-items-center rounded-full bg-accent px-1 font-mono text-[9.5px] font-bold tabular-nums text-fg-on-accent"
			>
				{activeCount}
			</span>
		{/if}
	</button>
</div>
