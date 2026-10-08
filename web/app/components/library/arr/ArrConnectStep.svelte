<script lang="ts">
	import { untrack } from "svelte";
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation } from "@tanstack/svelte-query";
	import { CircleCheck, Plug } from "@lucide/svelte";
	import { errorText } from "@lib/api";
	import { appLabel, previewSource } from "@lib/arr-import";
	import { arrConnectForm } from "@lib/schemas";
	import type { ArrApp, ArrPreview, ArrSourceRequest } from "@lib/types";
	import TextField from "@components/forms/TextField.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		app: ArrApp;
		url: string;
		apiKey: string;
		preview: ArrPreview | null;
		// Every edit is reported, so the wizard can drop a preview the new values
		// no longer describe.
		onCredentials: (url: string, apiKey: string) => void;
		onPreview: (p: ArrPreview) => void;
	};

	let { app, url, apiKey, preview, onCredentials, onPreview }: Props =
		$props();

	let label = $derived(appLabel(app));
	let example = $derived(
		app === "radarr" ? "http://radarr:7878" : "http://sonarr:8989",
	);

	const test = createMutation<ArrPreview, Error, ArrSourceRequest>(() => ({
		mutationFn: previewSource,
		onSuccess: (p, req) => {
			// An answer to values that have since been edited describes an instance
			// the operator has moved away from; keep it out of the wizard.
			if (req.url !== url || req.api_key !== apiKey) return;
			onPreview(p);
		},
	}));

	const form = createForm(() => ({
		// Seeded from the wizard so stepping back here keeps what was typed.
		// Untracked: createForm re-reads these options on every change they
		// depend on, and the wizard's copy moves with each keystroke.
		defaultValues: untrack(() => ({ url, api_key: apiKey })),
		validators: { onChange: arrConnectForm },
		listeners: {
			onChange: ({ formApi }) => {
				test.reset();
				onCredentials(formApi.state.values.url, formApi.state.values.api_key);
			},
		},
		onSubmit: ({ value }) =>
			test.mutate({ app, url: value.url, api_key: value.api_key }),
	}));
</script>

<form
	class="space-y-4"
	onsubmit={(e) => {
		e.preventDefault();
		form.handleSubmit();
	}}
>
	<form.Field name="url">
		{#snippet children(field)}
			<TextField
				{field}
				label={i18n.arr_url_label({ app: label })}
				placeholder={example}
				autocomplete="off"
				help={i18n.arr_url_help({ app: label, example })}
			/>
		{/snippet}
	</form.Field>

	<form.Field name="api_key">
		{#snippet children(field)}
			<TextField
				{field}
				type="password"
				label={i18n.arr_api_key_label()}
				autocomplete="off"
				help={i18n.arr_api_key_help({ app: label })}
			/>
		{/snippet}
	</form.Field>

	<div class="flex flex-col gap-3 sm:flex-row sm:items-start">
		<button
			type="submit"
			disabled={test.isPending}
			class="inline-flex min-h-11 shrink-0 items-center justify-center gap-1.5 rounded-md border border-border bg-bg-base px-3 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-progress disabled:opacity-60 lg:h-9 lg:min-h-0"
		>
			<Plug size={14} aria-hidden="true" />
			{test.isPending ? i18n.arr_testing() : i18n.arr_test_connection()}
		</button>

		<div class="min-w-0 flex-1" aria-live="polite">
			{#if test.isError}
				<p role="alert" class="text-sm break-words text-status-failed">
					{errorText(test.error)}
				</p>
			{:else if preview}
				<p
					class="flex items-center gap-1.5 text-sm font-medium text-status-available"
				>
					<CircleCheck size={15} class="shrink-0" aria-hidden="true" />
					<span class="min-w-0 truncate"
						>{i18n.arr_connected({ app: label, version: preview.version })}</span
					>
				</p>
				<p class="mt-1 text-xs text-fg-muted">
					{i18n.arr_counts({
						titles: preview.counts.titles,
						with_file: preview.counts.with_file,
						monitored: preview.counts.monitored,
					})}
				</p>
			{/if}
		</div>
	</div>
</form>
