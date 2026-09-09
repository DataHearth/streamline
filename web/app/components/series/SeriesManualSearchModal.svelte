<script lang="ts">
	import Modal from "../modals/Modal.svelte";
	import ReleasesTable from "../shared/ReleasesTable.svelte";
	import ReplaceExistingToggle from "../shared/ReplaceExistingToggle.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		open,
		seriesId,
		episodeId,
		scopeLabel,
		onClose,
	}: {
		open: boolean;
		seriesId: number;
		episodeId: number;
		// e.g. "S05E03 — Hazard Pay"; shown in the modal title for context.
		scopeLabel?: string;
		onClose: () => void;
	} = $props();

	let replaceExisting = $state(false);
	$effect(() => {
		if (open) replaceExisting = false;
	});
</script>

{#snippet replaceFooter()}
	<!-- P2: the grab modifier sits under the list, next to where the eye ends
	     up after picking a row, and stays put while a long list scrolls. The
	     help text was only ever a title attribute before. -->
	<div class="flex w-full items-center">
		<ReplaceExistingToggle
			checked={replaceExisting}
			onChange={(v) => (replaceExisting = v)}
		/>
	</div>
{/snippet}

<Modal
	{open}
	title={scopeLabel ? i18n.manual_search_scope({ scope: scopeLabel }) : i18n.action_manual_search()}
	size="4xl"
	{onClose}
	footer={episodeId > 0 ? replaceFooter : undefined}
>
	{#if episodeId > 0}
		<ReleasesTable
			searchPath={`/series/${seriesId}/episodes/${episodeId}/search`}
			grabPath={`/series/${seriesId}/episodes/${episodeId}/grab`}
			queryKey={["releases", "episode", episodeId]}
			{replaceExisting}
			enabled={open}
			onGrabbed={onClose}
		/>
	{/if}
</Modal>
