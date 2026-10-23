<script lang="ts">
	import { untrack } from "svelte";
	import { parts } from "@lib/message-parts";
	import Dialog from "@components/modals/Dialog.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import type { Torrent } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Removing a torrent asks whether its files go too, every time, unticked:
	// shared by the drawer's Remove button and the touch row's swipe.
	const removeBody = parts(i18n.torrent_remove_body, ["name"]);
	const deleteFilesBody = parts(i18n.torrent_delete_files_body, ["action"]);

	let {
		torrent,
		onClose,
		onRemove,
	}: {
		// null keeps the dialog closed.
		torrent: Torrent | null;
		onClose: () => void;
		onRemove: (hash: string, deleteFiles: boolean) => void;
	} = $props();

	// Unticked again whenever a dialog opens. Keyed on the hash, not the object:
	// the list refetches every 2 s, and a fresh object mid-question would clear
	// the box under the user's thumb.
	let deleteFiles = $state(false);
	let openFor = $derived(torrent?.hash ?? null);
	$effect(() => {
		openFor;
		untrack(() => (deleteFiles = false));
	});
</script>

<Dialog
	open={torrent !== null}
	title={i18n.torrent_remove_confirm()}
	inlineActions
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_remove(),
			variant: "danger",
			onClick: () => torrent && onRemove(torrent.hash, deleteFiles),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{#each removeBody as p}{#if p.slot === "name"}<span class="font-medium text-fg">{torrent?.name || i18n.torrent_this()}</span>{:else}{p.text}{/if}{/each}
	</p>
	<Checkbox
		checked={deleteFiles}
		onChange={(v) => (deleteFiles = v)}
		tone="danger"
		class="mt-4 rounded-md border border-border bg-bg-card p-3"
	>
		<span class="text-sm text-fg-muted">
			{#each deleteFilesBody as p}{#if p.slot === "action"}<span class="font-medium text-fg">{i18n.torrent_also_delete_files()}</span>{:else}{p.text}{/if}{/each}
		</span>
	</Checkbox>
</Dialog>
