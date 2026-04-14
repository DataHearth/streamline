<script lang="ts">
	import Modal from "../modals/Modal.svelte";
	import ReleasesTable from "../movies/ReleasesTable.svelte";
	import ReplaceExistingToggle from "../movies/ReplaceExistingToggle.svelte";

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

<Modal
	{open}
	title={scopeLabel ? `Manual search · ${scopeLabel}` : "Manual search"}
	size="4xl"
	{onClose}
>
	{#if episodeId > 0}
		<div class="mb-4 flex justify-end">
			<ReplaceExistingToggle
				checked={replaceExisting}
				onChange={(v) => (replaceExisting = v)}
			/>
		</div>
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
