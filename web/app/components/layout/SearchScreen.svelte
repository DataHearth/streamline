<script lang="ts">
	import { onMount, tick } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Search, SearchX, X } from "@lucide/svelte";
	import {
		createSearchModel,
		searchNav,
		type SearchItem,
	} from "../../lib/search-model.svelte";
	import SearchResultRow from "./SearchResultRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Phone only — AppShell renders this in place of CommandPalette below md,
	// off the same matchMedia switch it already runs for the add flow. Both
	// listen for `streamline:open-palette`, so the top bar dispatches one event
	// and doesn't know which surface answers.
	//
	// A screen rather than a centred dialog: the keyboard takes roughly half a
	// phone, which leaves a 640px box nowhere to sit. The field goes where the
	// hands are — top of the screen, keyboard up on open — and results fill
	// what's left.
	let open = $state(false);
	let query = $state("");
	let inputEl = $state<HTMLInputElement | null>(null);

	const model = createSearchModel(() => query, { compact: true });
	const navigateTo = searchNav();

	async function show() {
		if (open) return;
		open = true;
		query = "";
		await tick();
		inputEl?.focus();
	}

	function close() {
		open = false;
		query = "";
	}

	function pick(item: SearchItem) {
		close();
		navigateTo(item);
	}

	// The keyboard's Search key takes the first result, which is what the row
	// order is for.
	function onSubmit(e: Event) {
		e.preventDefault();
		const first = model.flat[0];
		if (first) pick(first);
		else inputEl?.blur();
	}

	let typed = $derived(query.trim().length > 0);

	onMount(() => {
		const onOpen = () => show();
		window.addEventListener("streamline:open-palette", onOpen);
		return () => window.removeEventListener("streamline:open-palette", onOpen);
	});

	$effect(() => {
		if (!open) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") close();
		};
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	});
</script>

{#if open}
	<div
		role="dialog"
		aria-modal="true"
		aria-label={i18n.common_search()}
		transition:fly={{ y: 16, duration: 200, easing: cubicOut }}
		class="fixed inset-0 z-50 flex flex-col bg-bg-deep md:hidden"
	>
		<form
			onsubmit={onSubmit}
			class="flex flex-none items-center gap-3 px-4 pb-2.5 pt-[max(env(safe-area-inset-top),10px)]"
		>
			<div
				class="search-box flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-xl border border-border-strong bg-surface px-3"
			>
				<Search
					size={17}
					class="search-icon shrink-0 text-fg-subtle"
					aria-hidden="true"
				/>
				<input
					bind:this={inputEl}
					bind:value={query}
					type="text"
					placeholder={i18n.search_your_library()}
					aria-label={i18n.search_your_library()}
					enterkeyhint="search"
					autocomplete="off"
					autocapitalize="off"
					spellcheck="false"
					class="search-input min-w-0 flex-1 border-0 bg-transparent text-[15px] text-fg placeholder:text-fg-faint"
				/>
				{#if typed}
					<button
						type="button"
						onclick={() => {
							query = "";
							inputEl?.focus();
						}}
						aria-label={i18n.common_clear_search()}
						class="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-surface-2 text-fg-subtle transition hover:text-fg"
					>
						<X size={13} aria-hidden="true" />
					</button>
				{/if}
			</div>
			<button
				type="button"
				onclick={close}
				class="touch-hit flex-none text-[14.5px] font-medium text-accent-text"
			>
				{i18n.common_cancel()}
			</button>
		</form>

		<div
			class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-1.5 pb-[max(env(safe-area-inset-bottom),16px)]"
		>
			{#each model.sections as section (section.id)}
				<div
					class="flex items-baseline justify-between px-3 pb-1 pt-3 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
				>
					<span>{section.label}</span>
					{#if section.id === "titles" && model.searchable !== null}
						<span class="text-[10px] normal-case tracking-[0.04em]">
							{model.titleHits} of {model.searchable.toLocaleString()}
						</span>
					{/if}
				</div>
				{#each section.items as item, i (item.kind + "-" + i)}
					<SearchResultRow {item} onpick={pick} />
				{/each}
			{/each}

			{#if model.flat.length === 0}
				<div class="flex flex-col items-center gap-2.5 px-6 py-14 text-center">
					<SearchX size={26} class="text-fg-faint" aria-hidden="true" />
					<p class="text-[13.5px] text-fg-subtle">
						No matches for “{query.trim()}”
					</p>
				</div>
			{/if}
		</div>
	</div>
{/if}

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
