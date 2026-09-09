<script lang="ts">
	import Modal from "../modals/Modal.svelte";
	import ReleasesTable from "../shared/ReleasesTable.svelte";
	import ReplaceExistingToggle from "../shared/ReplaceExistingToggle.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		open,
		movieId,
		scopeLabel,
		onClose,
	}: {
		open: boolean;
		movieId: number;
		// e.g. "Dune (2021)"; shown in the modal title for context.
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
	title={scopeLabel
		? i18n.manual_search_scope({ scope: scopeLabel })
		: i18n.action_manual_search()}
	size="4xl"
	{onClose}
	footer={replaceFooter}
>
	<ReleasesTable
		searchPath={`/movies/${movieId}/search`}
		grabPath={`/movies/${movieId}/grab`}
		queryKey={["releases", "movie", movieId]}
		{replaceExisting}
		enabled={open}
		onGrabbed={onClose}
	/>
</Modal>
