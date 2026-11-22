<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import NewImportForm from "./NewImportForm.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Below md the new-scan form is a sheet, not a modal: it is the same
	// vocabulary every other surface a phone opens in this app uses, and a
	// centred lg dialog at 390px is a full-screen card with rounded corners and
	// a scrim that never shows.
	let {
		open,
		onClose,
	}: { open: boolean; onClose: () => void } = $props();

</script>

{#if open}
	<BottomSheet
		title={i18n.imports_start_new_scan()}
		label={i18n.imports_start_new_scan()}
		{onClose}
		hideFrom="md"
		class="max-h-[92dvh]"
		headerClass="pb-3"
		bodyClass="border-t border-border pb-[max(env(safe-area-inset-bottom),18px)] pt-4"
	>
		<NewImportForm onCreated={onClose} />
	</BottomSheet>
{/if}
