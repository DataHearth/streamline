<script lang="ts">
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Search, X } from "@lucide/svelte";
	import {
		createSearchModel,
		searchNav,
		type SearchItem,
	} from "../../lib/search-model.svelte";
	import SearchResultRow from "./SearchResultRow.svelte";

	// Tablet band (md → lg). The width is there for a real field, so the bar gets
	// one instead of an icon, and the results drop as a panel under it — the one
	// place where being a dropdown beats taking the screen, since the grid stays
	// visible behind it. Below md the top bar keeps the icon and the phone
	// screen answers; from lg the palette does.
	let query = $state("");
	let panelOpen = $state(false);
	let inputEl = $state<HTMLInputElement | null>(null);
	let wrap = $state<HTMLDivElement | null>(null);

	const model = createSearchModel(() => query, { compact: true });
	const navigateTo = searchNav();

	let typed = $derived(query.trim().length > 0);
	let showPanel = $derived(panelOpen && typed);

	function close() {
		panelOpen = false;
	}

	function pick(item: SearchItem) {
		query = "";
		close();
		inputEl?.blur();
		navigateTo(item);
	}

	function onSubmit(e: Event) {
		e.preventDefault();
		const first = model.flat[0];
		if (first) pick(first);
	}

	$effect(() => {
		if (!showPanel) return;
		const onDown = (e: MouseEvent) => {
			if (!wrap?.contains(e.target as Node)) close();
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "Escape") return;
			if (typed) query = "";
			else inputEl?.blur();
			close();
		};
		document.addEventListener("mousedown", onDown);
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("mousedown", onDown);
			document.removeEventListener("keydown", onKey);
		};
	});
</script>

<div
	bind:this={wrap}
	class="relative hidden min-w-0 max-w-[420px] flex-1 md:block lg:hidden"
>
	<form
		onsubmit={onSubmit}
		class="search-box flex h-11 items-center gap-2.5 rounded-xl border border-border bg-surface px-3.5 transition"
	>
		<Search
			size={16}
			class="search-icon shrink-0 text-fg-subtle"
			aria-hidden="true"
		/>
		<input
			bind:this={inputEl}
			bind:value={query}
			onfocus={() => (panelOpen = true)}
			oninput={() => (panelOpen = true)}
			type="text"
			placeholder="Find a movie, release, indexer…"
			aria-label="Search"
			enterkeyhint="search"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			class="search-input min-w-0 flex-1 border-0 bg-transparent text-[13.5px] text-fg placeholder:text-fg-faint"
		/>
		{#if typed}
			<button
				type="button"
				onclick={() => {
					query = "";
					inputEl?.focus();
				}}
				aria-label="Clear search"
				class="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-surface-2 text-fg-subtle transition hover:text-fg"
			>
				<X size={13} aria-hidden="true" />
			</button>
		{/if}
	</form>

	{#if showPanel}
		<div
			role="listbox"
			aria-label="Search results"
			transition:fly={{ y: -4, duration: 160, easing: cubicOut }}
			class="absolute right-0 top-[calc(100%+8px)] z-50 max-h-[420px] w-[520px] overflow-y-auto overscroll-contain rounded-xl border border-border-strong bg-bg-elevated p-1.5 shadow-4"
		>
			{#each model.sections as section (section.id)}
				<div
					class="flex items-baseline justify-between px-2.5 pb-1 pt-2 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
				>
					<span>{section.label}</span>
					{#if section.id === "titles" && model.searchable !== null}
						<span class="text-[10px] normal-case tracking-[0.04em]">
							{model.titleHits} of {model.searchable.toLocaleString()}
						</span>
					{/if}
				</div>
				{#each section.items as item, i (item.kind + "-" + i)}
					<SearchResultRow {item} dense onpick={pick} />
				{/each}
			{/each}

			{#if model.flat.length === 0}
				<div class="px-3 py-7 text-center text-[12.5px] text-fg-subtle">
					No matches for “{query.trim()}”
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.search-box:focus-within {
		border-color: var(--accent-line);
		background: var(--bg-card);
		box-shadow: 0 0 0 3px var(--accent-soft);
	}
	.search-box:focus-within :global(.search-icon) {
		color: var(--accent);
	}
	.search-input:focus,
	.search-input:focus-visible {
		outline: none;
		box-shadow: none;
	}
</style>
