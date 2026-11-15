<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import {
		ACCOUNT_EXTENSIONS_KEY,
		EXTENSIONS_KEY,
		NO_PERMISSIONS,
		diffPermissions,
		extensionPath,
		extensionYaml,
		grantsOf,
		hasFullAccess,
		isEmpty,
		type Extension,
	} from "@lib/extensions";
	import Dialog from "@components/modals/Dialog.svelte";
	import ExtensionPermissions from "./ExtensionPermissions.svelte";
	import ExtensionYaml from "./ExtensionYaml.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A module asking for more than its entry holds — typically a new version —
	// waits here. What is new is set against what is already granted, and the
	// one action grants exactly what the module asks for: no more, and nothing
	// it stopped asking for.
	let { ext, onClose }: { ext: Extension | null; onClose: () => void } = $props();

	// Kept past `ext` going null so the dialog does not empty as it closes.
	let shown = $state<Extension | null>(null);
	$effect.pre(() => {
		if (ext) shown = ext;
	});

	let diff = $derived(
		shown?.requested
			? diffPermissions(shown.requested, shown.granted)
			: { added: NO_PERMISSIONS, kept: NO_PERMISSIONS, dropped: NO_PERMISSIONS },
	);

	const qc = useQueryClient();
	const approve = createMutation<Extension, Error, Extension>(() => ({
		mutationFn: (x) => api<Extension>(`${extensionPath(x.name)}/approve`, { method: "POST" }),
		onSuccess: (x) => {
			qc.invalidateQueries({ queryKey: EXTENSIONS_KEY });
			qc.invalidateQueries({ queryKey: ACCOUNT_EXTENSIONS_KEY });
			toast.ok(i18n.extensions_approved({ name: x.name }));
			onClose();
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	let yaml = $derived(
		shown?.requested
			? extensionYaml({ name: shown.name, source: shown.source, sha256: shown.sha256, enabled: shown.enabled, grants: grantsOf(shown.requested) })
			: "",
	);

	// The block that names ffmpeg is first among the new permissions, so the
	// button that grants it says so.
	let fullAccess = $derived(hasFullAccess(diff.added));

	const head = "flex items-center gap-2 text-[13px] font-semibold";
</script>

<Dialog
	open={ext !== null}
	title={i18n.extensions_approve_title({ name: shown?.name ?? "", version: shown?.version ? `v${shown.version}` : "" })}
	size="lg"
	{onClose}
	actions={config.readOnly
		? [{ label: i18n.common_close(), variant: "ghost", autofocus: true }]
		: [
				{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
				{
					label: fullAccess ? i18n.extensions_approve_full_access() : i18n.extensions_approve(),
					variant: fullAccess ? "danger" : "primary",
					dismiss: false,
					pending: approve.isPending,
					onClick: () => shown && approve.mutate(shown),
				},
			]}
>
	{#if shown}
		<p class="text-sm leading-relaxed text-fg-muted">{i18n.extensions_approve_lead()}</p>

		<div class="mt-4 flex flex-col gap-4">
			{#if !isEmpty(diff.added)}
				<section class="rounded-md border border-status-wanted/30 bg-status-wanted/[0.06] p-3">
					<h3 class="{head} text-status-wanted">{i18n.extensions_new()}</h3>
					<ExtensionPermissions permissions={diff.added} mark="added" class="mt-2.5" />
				</section>
			{/if}
			{#if !isEmpty(diff.kept)}
				<section class="px-3">
					<h3 class="{head} text-fg-muted">{i18n.extensions_already_granted()}</h3>
					<ExtensionPermissions permissions={diff.kept} mark="kept" class="mt-2.5" />
				</section>
			{/if}
			{#if !isEmpty(diff.dropped)}
				<section class="px-3">
					<h3 class="{head} text-fg-muted">{i18n.extensions_dropped()}</h3>
					<ExtensionPermissions permissions={diff.dropped} mark="dropped" class="mt-2.5" />
				</section>
			{/if}
		</div>

		{#if config.readOnly}
			<div class="mt-5">
				<ExtensionYaml {yaml} lead={i18n.extensions_yaml_replace()} />
			</div>
		{/if}
	{/if}
</Dialog>
