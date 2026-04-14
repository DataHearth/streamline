<script lang="ts">
	import { onMount } from "svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { auth } from "../lib/auth.svelte.js";
	import { config } from "../lib/config.svelte.js";
	import AppShell from "../components/layout/AppShell.svelte";

	type IsActiveFn = (path: string) => boolean;
	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	// Auth + error pages are full-bleed: no sidebar, no nav, no user hydration.
	// Anything else gets the shell.
	let bare = $derived(
		isActiveFn("/login") ||
			isActiveFn("/register") ||
			isActiveFn("/forbidden"),
	);

	onMount(() => {
		if (!bare) {
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
	<AppShell>
		<!-- svelte-ignore slot_element_deprecated -->
		<slot />
	</AppShell>
{/if}
