<script lang="ts">
	import Modal from "../modals/Modal.svelte";
	import ReleasesTable from "./ReleasesTable.svelte";
	import ReplaceExistingToggle from "./ReplaceExistingToggle.svelte";

	let {
		open,
		movieId,
		onClose,
	}: {
		open: boolean;
		movieId: number;
		onClose: () => void;
	} = $props();

	let replaceExisting = $state(false);
	$effect(() => {
		if (open) replaceExisting = false;
	});
</script>

<Modal {open} title="Manual search" size="4xl" {onClose}>
	<div class="mb-4 flex justify-end">
		<ReplaceExistingToggle
			checked={replaceExisting}
			onChange={(v) => (replaceExisting = v)}
		/>
	</div>
	<ReleasesTable
		searchPath={`/movies/${movieId}/search`}
		grabPath={`/movies/${movieId}/grab`}
		queryKey={["releases", "movie", movieId]}
		{replaceExisting}
		enabled={open}
		onGrabbed={onClose}
	/>
</Modal>
