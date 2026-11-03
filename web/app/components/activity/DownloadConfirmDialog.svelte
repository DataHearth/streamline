<script lang="ts" module>
	export type DownloadConfirm = "cancel" | "retry" | "remove";
</script>

<script lang="ts">
	import { parts } from "@lib/message-parts";
	import Dialog from "@components/modals/Dialog.svelte";
	import type { HistoryEntry, QueueEntry } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The three questions a download record asks before a verb that cannot be
	// taken back, shared by the detail sheet's buttons and the touch row's
	// swipes so both ask in the same words.
	const cancelBody = parts(i18n.activity_cancel_body, ["title", "status"]);
	const retryBody = parts(i18n.activity_retry_body, ["title"]);
	const deleteEntryBody = parts(i18n.activity_delete_entry_body, ["title"]);

	let {
		kind,
		item,
		onClose,
		onConfirm,
	}: {
		// null keeps the dialog closed.
		kind: DownloadConfirm | null;
		item: QueueEntry | HistoryEntry | null;
		onClose: () => void;
		onConfirm: (kind: DownloadConfirm, id: number) => void;
	} = $props();

	function confirm(k: DownloadConfirm) {
		if (item) onConfirm(k, item.id);
	}
</script>

<Dialog
	open={kind === "cancel" && item !== null}
	title={i18n.activity_cancel_confirm()}
	inlineActions
	{onClose}
	actions={[
		{ label: i18n.common_keep(), variant: "ghost", autofocus: true },
		{
			label: i18n.action_cancel_download(),
			variant: "danger",
			onClick: () => confirm("cancel"),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{#each cancelBody as p}{#if p.slot === "title"}<span class="font-medium text-fg">{item?.title}</span>{:else if p.slot === "status"}<em>{i18n.lc_wanted()}</em>{:else}{p.text}{/if}{/each}
	</p>
</Dialog>

<Dialog
	open={kind === "retry" && item !== null}
	title={i18n.activity_retry_import_confirm()}
	inlineActions
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.activity_retry_import(),
			variant: "primary",
			onClick: () => confirm("retry"),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{#each retryBody as p}{#if p.slot === "title"}<span class="font-medium text-fg">{item?.title}</span>{:else}{p.text}{/if}{/each}
	</p>
</Dialog>

<Dialog
	open={kind === "remove" && item !== null}
	title={i18n.activity_remove_record_confirm()}
	inlineActions
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_remove(),
			variant: "danger",
			onClick: () => confirm("remove"),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{#each deleteEntryBody as p}{#if p.slot === "title"}<span class="font-medium text-fg">{item?.title}</span>{:else}{p.text}{/if}{/each}
	</p>
</Dialog>
