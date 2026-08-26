<script lang="ts">
	import { useIsFetching, useIsMutating } from "@tanstack/svelte-query";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const fetching = useIsFetching();
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
	     search runs inside a modal, which is exactly where the stall was felt. -->
	<div class="fixed inset-x-0 top-0 z-[300]">
		<ProgressBar height={2} label={i18n.common_loading()} />
	</div>
{/if}
