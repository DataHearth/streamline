<script lang="ts">
	import Modal from "@components/modals/Modal.svelte";
	import ReleasesTable from "@components/shared/ReleasesTable.svelte";
	import type { ExistingFile } from "@lib/release-facts";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		open,
		movieId,
		scopeLabel,
		existing = null,
		onClose,
	}: {
		open: boolean;
		movieId: number;
		// e.g. "Dune (2021)"; shown in the modal title for context.
		scopeLabel?: string;
		// The movie's file on disk, if any: picking a result then asks before
		// replacing it.
		existing?: ExistingFile | null;
		onClose: () => void;
	} = $props();
</script>

<Modal
	{open}
	title={scopeLabel
		? i18n.manual_search_scope({ scope: scopeLabel })
		: i18n.action_manual_search()}
	size="4xl"
	{onClose}
>
	<ReleasesTable
		searchPath={`/movies/${movieId}/search`}
		grabPath={`/movies/${movieId}/grab`}
		queryKey={["releases", "movie", movieId]}
		{existing}
		enabled={open}
		onGrabbed={onClose}
	/>
</Modal>
