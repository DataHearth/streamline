<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { TriangleAlert, Check } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { metadataConfigPatch } from "../../lib/schemas";
	import type { MetadataConfig, MetadataConfigPatch } from "../../lib/types";
	import TextField from "../../components/forms/TextField.svelte";
	import SubmitButton from "../../components/forms/SubmitButton.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const cfg = createQuery<MetadataConfig>(() => ({
		queryKey: ["config", "metadata"],
		queryFn: () => api<MetadataConfig>("/config/metadata"),
	}));

	const save = createMutation<MetadataConfig, Error, MetadataConfigPatch>(
		() => ({
			mutationFn: (body) =>
				api<MetadataConfig>("/config/metadata", { method: "PATCH", body }),
			onSuccess: (resp) => {
				qc.setQueryData(["config", "metadata"], resp);
				seedFrom(resp);
				toast.ok(i18n.metadata_saved());
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	const form = createForm(() => ({
		defaultValues: {
			language: cfg.data?.language ?? "en",
			tmdb_region: cfg.data?.tmdb_region ?? "",
			tmdb_api_key: "",
			tvdb_api_key: "",
		},
		validators: { onChange: metadataConfigPatch },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	// The keys are never echoed, so they always seed blank — blank is what the
	// API reads as "leave the stored one alone".
	function seedFrom(data: MetadataConfig) {
		form.reset({
			language: data.language,
			tmdb_region: data.tmdb_region,
			tmdb_api_key: "",
			tvdb_api_key: "",
		});
	}

	// Same guard as /settings/auth: the query refetches on window focus, so
	// re-seed only while the form is clean or a refetch throws away typing.
	$effect(() => {
		if (!cfg.data || form.state.isDirty) return;
		seedFrom(cfg.data);
	});

	function keyPlaceholder(set: boolean, fileManaged: boolean) {
		if (fileManaged) return i18n.metadata_key_from_file();
		return set ? i18n.metadata_key_stored() : i18n.metadata_key_unset();
	}
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.settings_metadata()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">{i18n.settings_metadata_intro()}</p>
	</header>

	{#if cfg.isPending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(cfg.error) })}
		</p>
	{:else if cfg.data}
		{@const data = cfg.data}
		{#if data.restart_required}
			<div
				class="mt-6 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
			>
				<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<div>
					<p class="font-medium">{i18n.settings_restart_required()}</p>
					<p class="mt-0.5 text-status-wanted/80">
						{i18n.settings_changes_after_restart()}
					</p>
				</div>
			</div>
		{/if}

		<form
			class="mt-6"
			onsubmit={(e) => {
				e.preventDefault();
				form.handleSubmit();
			}}
		>
			<ReadOnlyFieldset class="space-y-6">
				<section class="space-y-4 rounded-lg border border-border bg-bg-card p-4">
					<div>
						<h2 class="text-sm font-semibold text-fg">
							{i18n.metadata_providers()}
						</h2>
						<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
							{i18n.metadata_providers_help()}
						</p>
					</div>

					<div class="grid gap-4 sm:grid-cols-2">
						<form.Field name="tmdb_api_key">
							{#snippet children(field)}
								<TextField
									{field}
									type="password"
									autocomplete="off"
									label={i18n.metadata_tmdb_key()}
									readonly={data.tmdb_api_key_file_managed ?? false}
									placeholder={keyPlaceholder(
										data.tmdb_api_key_set,
										data.tmdb_api_key_file_managed ?? false,
									)}
									help={i18n.metadata_tmdb_key_help()}
								/>
							{/snippet}
						</form.Field>

						<form.Field name="tvdb_api_key">
							{#snippet children(field)}
								<TextField
									{field}
									type="password"
									autocomplete="off"
									label={i18n.metadata_tvdb_key()}
									readonly={data.tvdb_api_key_file_managed ?? false}
									placeholder={keyPlaceholder(
										data.tvdb_api_key_set,
										data.tvdb_api_key_file_managed ?? false,
									)}
									help={i18n.metadata_tvdb_key_help()}
								/>
							{/snippet}
						</form.Field>
					</div>
				</section>

				<section class="space-y-4 rounded-lg border border-border bg-bg-card p-4">
					<div>
						<h2 class="text-sm font-semibold text-fg">
							{i18n.metadata_localisation()}
						</h2>
						<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
							{i18n.metadata_localisation_help()}
						</p>
					</div>

					<div class="grid gap-4 sm:grid-cols-2">
						<form.Field name="language">
							{#snippet children(field)}
								<TextField
									{field}
									label={i18n.metadata_language()}
									placeholder="en"
									help={i18n.metadata_language_help()}
								/>
							{/snippet}
						</form.Field>

						<form.Field name="tmdb_region">
							{#snippet children(field)}
								<TextField
									{field}
									label={i18n.metadata_region()}
									placeholder="FR"
									help={i18n.metadata_region_help()}
								/>
							{/snippet}
						</form.Field>
					</div>
				</section>

				<div class="flex items-center justify-end gap-2">
					<span class="inline-flex items-center gap-1.5 text-xs text-fg-subtle">
						<Check size={12} aria-hidden="true" />
						{i18n.metadata_applies_on_restart()}
					</span>
					<SubmitButton
						{form}
						label={i18n.common_save_changes()}
						pendingLabel={i18n.common_saving()}
						disabled={config.readOnly}
						title={config.readOnly ? READONLY_HINT : undefined}
					/>
				</div>
			</ReadOnlyFieldset>
		</form>
	{/if}
</div>
