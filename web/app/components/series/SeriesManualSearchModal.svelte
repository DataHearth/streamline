<script lang="ts">
	import Modal from "@components/modals/Modal.svelte";
	import ReleasesTable from "@components/shared/ReleasesTable.svelte";
	import type { ExistingFile } from "@lib/release-facts";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		open,
		seriesId,
		episodeId,
		scopeLabel,
		existing = null,
		onClose,
	}: {
		open: boolean;
		seriesId: number;
		episodeId: number;
		// e.g. "S05E03 — Hazard Pay"; shown in the modal title for context.
		scopeLabel?: string;
		// The episode's file on disk, if any: picking a result then asks before
		// replacing it.
		existing?: ExistingFile | null;
		onClose: () => void;
	} = $props();
</script>

<Modal
	{open}
	title={scopeLabel ? i18n.manual_search_scope({ scope: scopeLabel }) : i18n.action_manual_search()}
	size="4xl"
	{onClose}
>
	{#if episodeId > 0}
		<ReleasesTable
			searchPath={`/series/${seriesId}/episodes/${episodeId}/search`}
			grabPath={`/series/${seriesId}/episodes/${episodeId}/grab`}
			queryKey={["releases", "episode", episodeId]}
			{existing}
			enabled={open}
			onGrabbed={onClose}
		/>
	{/if}
</Modal>
