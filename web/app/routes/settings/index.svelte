<script lang="ts">
	import { onMount } from "svelte";
	import { goto } from "@roxi/routify";
	import { createSettingsDesktop } from "../../lib/viewport.svelte";
	import SettingsIndex from "../../components/settings/SettingsIndex.svelte";

	// /settings is two things now. From lg the sidebar owns the section list, so
	// landing here with no section chosen is meaningless and it redirects to
	// General as before. Below lg there is no sidebar and this IS the navigation.
	//
	// The redirect waits for the media query to resolve — firing it on the
	// server-default (false) would bounce every phone straight to General and
	// the index would never be reachable.
	const desktop = createSettingsDesktop();
	let mounted = $state(false);
	let gotoFn = $state<(p: string) => void>(() => {});
	onMount(() => {
		mounted = true;
		return goto.subscribe((fn) => (gotoFn = fn));
	});

	$effect(() => {
		if (mounted && desktop()) gotoFn("/settings/general");
	});
</script>

{#if !desktop()}
	<SettingsIndex />
{/if}
