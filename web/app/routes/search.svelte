<script lang="ts">
	import { onMount } from "svelte";
	import { get } from "svelte/store";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { activeRoute, goto } from "@roxi/routify";
	import { LoaderCircle, Search, SearchX, X } from "@lucide/svelte";
	import {
		createSearchModel,
		searchNav,
		type SearchItem,
	} from "@lib/search-model.svelte";
	import { onRouteQuery } from "@lib/route-query";
	import SearchResultRow from "@components/layout/SearchResultRow.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The phone's search, a page of its own: the top bar's search button links
	// here below md, where the command palette has no room. It used to be a
	// screen AppShell drew over the current page, which put nothing in history
	// — Back left the page underneath and the search stayed on top of the next
	// one (#140). As a route, Back and Cancel return to the page it was opened
	// from (AppShell restores its scroll position), and the typed query is in
	// the URL, so Back from a picked result lands on the same results.
	//
	// A screen rather than a centred dialog: the keyboard takes roughly half a
	// phone, which leaves a 640px box nowhere to sit. The field goes where the
	// hands are — top of the screen, keyboard up on open — and results fill
	// what's left.
	let query = $state("");
	let inputEl = $state<HTMLInputElement | null>(null);

	const model = createSearchModel(() => query, { compact: true });
	const navigateTo = searchNav();

	// Reached from another page of the app, or loaded straight from the URL:
	// only the first has somewhere for Back to go that is still the app.
	const opened = get(activeRoute);
	const fromApp = !!opened?.prevRoute;

	let navigate:
		| ((
				path: string,
				params?: Record<string, string>,
				options?: { mode: "push" | "replace" },
		  ) => void)
		| null = null;
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	function leave() {
		if (fromApp) window.history.back();
		else navigate?.("/", {}, { mode: "replace" });
	}

	// An action opens a surface of its own over the page search was opened
	// from (the add flow) or leaves the app (sign out), so it runs once that
	// page is back rather than over the search page on its way out.
	function leaveThen(run: () => void) {
		let off: (() => void) | null = null;
		let done = false;
		off = activeRoute.subscribe((r) => {
			if (done || !r || (r.url ?? "").split("?")[0] === "/search") return;
			done = true;
			queueMicrotask(() => off?.());
			run();
		});
		leave();
	}

	function pick(item: SearchItem) {
		if (item.kind === "action") leaveThen(item.run);
		else navigateTo(item);
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

	onMount(() =>
		onRouteQuery("/search", (p) => {
			query = p.get("q") ?? "";
			// Coming back to results is for reading them; an empty field is for
			// typing, so that one gets the keyboard.
			if (!query) inputEl?.focus();
		}),
	);

	$effect(() => {
		const q = query.trim();
		// Routify mounts the incoming route before it updates window.location;
		// writing then would stamp this query onto the outgoing page's path.
		if (typeof window === "undefined" || window.location.pathname !== "/search") return;
		const next = q ? `/search?${new URLSearchParams({ q })}` : "/search";
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	onMount(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") leave();
		};
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	});
</script>

<div
	role="search"
	aria-label={i18n.common_search()}
	in:fly={{ y: 16, duration: 200, easing: cubicOut }}
	class="fixed inset-0 z-50 flex flex-col bg-bg-deep md:static md:z-auto md:min-h-[60dvh]"
>
	<form
		onsubmit={onSubmit}
		class="flex flex-none items-center gap-3 px-4 pb-2.5 pt-[max(env(safe-area-inset-top),10px)]"
	>
		<div
			class="search-box flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-xl border border-border-strong bg-surface px-3"
		>
			{#if model.pending}
				<LoaderCircle
					size={17}
					class="search-icon shrink-0 animate-spin text-accent"
					aria-hidden="true"
				/>
			{:else}
				<Search
					size={17}
					class="search-icon shrink-0 text-fg-subtle"
					aria-hidden="true"
				/>
			{/if}
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
			onclick={leave}
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
						{i18n.search_hits_of({
							hits: model.titleHits,
							total: model.searchable.toLocaleString(),
						})}
					</span>
				{/if}
			</div>
			{#each section.items as item, i (item.kind + "-" + i)}
				<SearchResultRow {item} onpick={pick} />
			{/each}
		{/each}

		{#if model.flat.length === 0}
			<div role="status" class="flex flex-col items-center gap-2.5 px-6 py-14 text-center">
				{#if model.pending}
					<LoaderCircle size={26} class="animate-spin text-fg-faint" aria-hidden="true" />
					<p class="text-[13.5px] text-fg-subtle">{i18n.common_searching()}</p>
				{:else}
					<SearchX size={26} class="text-fg-faint" aria-hidden="true" />
					<p class="text-[13.5px] text-fg-subtle">
						{i18n.search_no_matches_for({ query: query.trim() })}
					</p>
				{/if}
			</div>
		{/if}
	</div>
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
