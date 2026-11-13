<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { ACCOUNT_EXTENSIONS_KEY, EXTENSIONS_KEY, extensionPath, type Extension } from "@lib/extensions";
	import Dialog from "@components/modals/Dialog.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Deleting removes the entry and every grant with it. Its data stays unless
	// the admin ticks the box (`purge`), so a reinstall finds it again; turning
	// the extension off is the way to pause it, and never deletes anything.
	let { ext, onClose }: { ext: Extension | null; onClose: () => void } = $props();

	let shown = $state<Extension | null>(null);
	let purge = $state(false);
	let wasOpen = false;
	$effect.pre(() => {
		if (ext && !wasOpen) purge = false;
		if (ext) shown = ext;
		wasOpen = ext !== null;
	});

	const qc = useQueryClient();
	const remove = createMutation<null, Error, { name: string; purge: boolean }>(() => ({
		mutationFn: ({ name, purge }) => api<null>(`${extensionPath(name)}?purge=${purge}`, { method: "DELETE" }),
		onSuccess: (_r, { name }) => {
			qc.invalidateQueries({ queryKey: EXTENSIONS_KEY });
			qc.invalidateQueries({ queryKey: ACCOUNT_EXTENSIONS_KEY });
			toast.ok(i18n.extensions_deleted({ name }));
			onClose();
		},
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<Dialog
	open={ext !== null}
	title={i18n.extensions_delete_title({ name: shown?.name ?? "" })}
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: purge ? i18n.extensions_delete_with_data() : i18n.common_delete(),
			variant: "danger",
			dismiss: false,
			pending: remove.isPending,
			onClick: () => shown && remove.mutate({ name: shown.name, purge }),
		},
	]}
>
	{#if shown}
		<p class="text-sm leading-relaxed text-fg-muted">
			{shown.card ? i18n.extensions_delete_body_card({ title: shown.card.title }) : i18n.extensions_delete_body()}
		</p>
		<Checkbox
			class="mt-4"
			tone="danger"
			checked={purge}
			onChange={(v) => (purge = v)}
			label={i18n.extensions_purge()}
			description={i18n.extensions_purge_help()}
		/>
		<p class="mt-4 text-xs leading-relaxed text-fg-subtle">{i18n.extensions_delete_disable_hint()}</p>
	{/if}
</Dialog>
