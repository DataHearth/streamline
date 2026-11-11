<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { CircleAlert } from "@lucide/svelte";
	import { api, ApiError, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import {
		ACCOUNT_EXTENSIONS_KEY,
		EXTENSIONS_KEY,
		STATE_PILL,
		extensionPath,
		extensionYaml,
		grantsOf,
		shaProblem,
		sourceProblem,
		type Extension,
	} from "@lib/extensions";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import SourceFields from "./SourceFields.svelte";
	import ExtensionYaml from "./ExtensionYaml.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Edit is the update path: a new release's source and checksum. The server
	// reads the new module before keeping anything, so a wrong checksum is
	// refused with its reason and the running version carries on. A version
	// that asks for more than the entry holds stops until it is approved.
	let { ext, onClose }: { ext: Extension | null; onClose: () => void } = $props();

	let source = $state("");
	let sha256 = $state("");
	let tried = $state(false);
	let problem = $state<string | null>(null);

	// The extension being edited outlives `ext` going null, so the dialog keeps
	// its content while it fades out; each opening starts from the entry.
	let shown = $state<Extension | null>(null);
	let wasOpen = false;
	$effect.pre(() => {
		if (ext && (!wasOpen || ext.name !== shown?.name)) {
			shown = ext;
			source = ext.source;
			sha256 = ext.sha256 ?? "";
			tried = false;
			problem = null;
		}
		wasOpen = ext !== null;
	});

	const qc = useQueryClient();
	let body = $derived({
		source: source.trim(),
		sha256: sha256.trim().toLowerCase(),
	});

	const save = createMutation<Extension, Error, void>(() => ({
		mutationFn: () => api<Extension>(extensionPath(shown?.name ?? ""), { method: "PATCH", body }),
		onSuccess: (x) => {
			qc.invalidateQueries({ queryKey: EXTENSIONS_KEY });
			qc.invalidateQueries({ queryKey: ACCOUNT_EXTENSIONS_KEY });
			toast.ok(
				x.state === "needs_approval"
					? i18n.extensions_saved_waiting({ name: x.name })
					: i18n.extensions_saved({ name: x.name }),
			);
			onClose();
		},
		onError: (err) => {
			if (err instanceof ApiError && err.body?.code === "extension_unreadable") problem = err.message;
			else toast.err(errorText(err));
		},
	}));

	function submit() {
		tried = true;
		if (sourceProblem(source) || shaProblem(sha256, source) || config.readOnly) return;
		problem = null;
		save.mutate();
	}

	let yaml = $derived(
		shown
			? extensionYaml({ name: shown.name, ...body, enabled: shown.enabled, grants: grantsOf(shown.granted) })
			: "",
	);
</script>

<ConfigFormShell
	open={ext !== null}
	title={i18n.common_edit_name({ name: shown?.name ?? "" })}
	size="xl"
	formId="extension-edit-form"
	submitLabel={save.isPending ? i18n.common_saving() : i18n.common_save_changes()}
	submitDisabled={save.isPending}
	{onClose}
>
	<form
		id="extension-edit-form"
		novalidate
		onsubmit={(e) => {
			e.preventDefault();
			submit();
		}}
	>
		{#if shown}
			<p class="mb-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-subtle">
				<span>{i18n.extensions_installed_now()}</span>
				<span class="font-mono text-fg-muted">{shown.version ? `v${shown.version}` : i18n.extensions_version_unknown()}</span>
				<LabelPill token={STATE_PILL[shown.state].token} label={STATE_PILL[shown.state].label()} />
			</p>
			<SourceFields bind:source bind:sha256 showErrors={tried} idPrefix="ext-edit" />

			{#if problem}
				<div role="alert" class="mt-4 flex items-start gap-2.5 rounded-md border border-status-failed/30 bg-status-failed/[0.07] p-3">
					<CircleAlert size={15} class="mt-0.5 shrink-0 text-status-failed" aria-hidden="true" />
					<div class="min-w-0 flex-1">
						<p class="text-sm font-medium text-fg">{i18n.extensions_unreadable_kept()}</p>
						<p class="mt-1 break-words font-mono text-[12px] leading-relaxed text-fg-muted">{problem}</p>
					</div>
				</div>
			{/if}

			<p class="mt-4 text-xs leading-relaxed text-fg-subtle">{i18n.extensions_edit_note()}</p>

			{#if config.readOnly}
				<div class="mt-5">
					<ExtensionYaml {yaml} lead={i18n.extensions_yaml_replace()} />
				</div>
			{/if}
		{/if}
	</form>
</ConfigFormShell>
