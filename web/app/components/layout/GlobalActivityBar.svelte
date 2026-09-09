<script lang="ts">
	import { useIsFetching, useIsMutating } from "@tanstack/svelte-query";
	import { portal } from "../../lib/focus-trap";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Queries tagged `meta: { silent: true }` don't raise the bar: chrome that
	// refreshes itself on a timer (the sidebar badges) and the dashboard, which
	// is a glance rather than a thing you wait on. The bar is for a request the
	// user asked for and is now waiting through — a poll nobody triggered reads
	// as a stall rather than as progress, which is the failure it exists to fix.
	const fetching = useIsFetching({
		predicate: (q) => q.meta?.silent !== true,
	});
	const mutating = useIsMutating();
	let busy = $derived(fetching.current + mutating.current > 0);

	// Cache hits settle in a few milliseconds; a bar that flashes on those reads
	// as a glitch rather than as progress. Only a request slow enough to be
	// mistaken for a stall gets the indicator.
	let visible = $state(false);
	$effect(() => {
		if (!busy) {
			visible = false;
			return;
		}
		const t = setTimeout(() => {
			visible = true;
		}, 300);
		return () => clearTimeout(t);
	});
</script>

{#if visible}
	<!-- Above the modal layer (z-50) and the Select portal (z-200): an indexer
	     search runs inside a modal, which is exactly where the stall was felt.
	     z-index alone was not enough — the bar renders inside the app root, and
	     the modal portals to <body>, so its z-50 in the ROOT stacking context
	     beat a z-300 confined to an ancestor's. The bar was painted behind the
	     backdrop and came out blurred. It portals to <body> too now, where the
	     two are finally comparable. -->
	<div use:portal class="fixed inset-x-0 top-0 z-[300]">
		<ProgressBar height={2} label={i18n.common_loading()} />
	</div>
{/if}
