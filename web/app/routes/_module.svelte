<script lang="ts">
	import { onMount } from "svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { auth } from "../lib/auth.svelte.js";
	import { config } from "../lib/config.svelte.js";
	import AppShell from "../components/layout/AppShell.svelte";

	// Auth + error pages are full-bleed: no sidebar, no nav, no user hydration.
	// Anything else gets the shell.
	const BARE = ["/login", "/register", "/forbidden"];
	const isBare = (p: string) =>
		BARE.some((b) => p === b || p.startsWith(`${b}/`));

	// The pathname, not Routify's isActive, decides this. isActive answers false
	// for every route until Routify resolves the active one a tick after mount,
	// so a `bare` derived from it starts false even on /login — long enough for
	// AppShell to mount and its children to fire eight authenticated queries that
	// all 401 before the shell is torn down again. location.pathname is correct
	// synchronously on the first render; the subscription below is used only as a
	// "navigation happened" signal to re-read it.
	let path = $state(window.location.pathname);
	onMount(() =>
		routifyIsActive.subscribe(() => (path = window.location.pathname)),
	);

	let bare = $derived(isBare(path));

	onMount(() => {
		if (!isBare(window.location.pathname)) {
			auth.hydrate();
			config.hydrate();
		}
	});
</script>

<!--
	Keep `<slot />` here — Routify v3's ComposeFragments renders the active
	route into the layout via Svelte-4 slot semantics. In Svelte 5 a default
	slot does NOT auto-bridge to a `children` snippet prop on a runes-mode
	component when the parent is a legacy-mode renderer (Routify's), so
	switching to `{@render children?.()}` yields an empty layout.
	AppShell itself uses runes + `{@render children?.()}` because we
	instantiate it directly with `<AppShell>…</AppShell>` from this file.
-->
{#if bare}
	<!-- svelte-ignore slot_element_deprecated -->
	<slot />
{:else}
	{#snippet shellChildren()}
		<!-- svelte-ignore slot_element_deprecated -->
		<slot />
	{/snippet}
	<AppShell children={shellChildren} />
{/if}
