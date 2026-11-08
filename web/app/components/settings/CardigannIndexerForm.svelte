<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Info, TriangleAlert } from "@lucide/svelte";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import CardigannYamlModal from "@components/settings/CardigannYamlModal.svelte";
	import ReadOnlyFieldset from "@components/settings/ReadOnlyFieldset.svelte";
	import TestConnectionButton from "@components/settings/TestConnectionButton.svelte";
	import TextField from "@components/forms/TextField.svelte";
	import TogglePill from "@components/forms/TogglePill.svelte";
	import Select from "@components/forms/Select.svelte";
	import { api, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import { cardigannIndexerForm } from "@lib/schemas";
	import { indexerYaml } from "@lib/cardigann-yaml";
	import type {
		CardigannDefinition,
		CardigannSetting,
		CardigannUpdates,
		Indexer,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A built-in indexer's form, generated from its definition's settings.
	// file names the definition for a new one; editing names the indexer.
	type Props = {
		file: string | null;
		editing: Indexer | null;
		onClose: () => void;
	};
	let { file, editing, onClose }: Props = $props();

	type Values = {
		name: string;
		site: string;
		enabled: boolean;
		priority: number;
		private: boolean;
		proxy: string;
		settings: Record<string, string>;
	};

	let definition = $derived(editing?.definition ?? file);
	let open = $derived(definition !== null);
	// A read-only instance can still fill in a new indexer: the form shows
	// the entry to paste into the config file instead of saving it.
	let drafting = $derived(config.readOnly && editing === null);
	let yaml = $state<string | null>(null);

	const updates = createQuery<CardigannUpdates>(() => ({
		queryKey: ["cardigann", "updates"],
		queryFn: () => api<CardigannUpdates>("/cardigann/updates"),
		enabled: drafting && open,
		// Opening the catalog may have just read upstream; an answer cached
		// before it carries no commit to pin.
		staleTime: 0,
	}));

	// An existing indexer is edited against the version it carries, which an
	// update may not have reached; a new one against the catalog's.
	const def = createQuery<CardigannDefinition>(() => ({
		queryKey: ["cardigann", "definition", definition, editing?.name ?? null],
		queryFn: () =>
			api<CardigannDefinition>(
				`/cardigann/catalog/${encodeURIComponent(definition ?? "")}` +
					(editing ? `?indexer=${encodeURIComponent(editing.name)}` : ""),
			),
		enabled: definition !== null,
	}));

	const valued = (type: string) =>
		type === "text" || type === "password" || type === "checkbox" || type === "select";

	// A select's value must be one of its option keys, which a few upstream
	// defaults are not (`DESC` beside `desc`, `t` beside nt/ts/sz/i): the
	// engine's selectDefault takes a case-insensitive match, else the first
	// option, and the form seeds the same key or the server refuses it.
	function selectKey(s: CardigannSetting, value: string | undefined) {
		const options = s.options ?? [];
		if (value !== undefined && options.some((o) => o.key === value)) return value;
		const folded = options.find((o) => o.key.toLowerCase() === value?.toLowerCase());
		return folded?.key ?? options[0]?.key ?? "";
	}

	// A setting the config file reads from a file is shown, never sent: the
	// server refuses a value for it.
	const fileOf = (name: string) => editing?.settings?.[`${name}_file`];

	function seed(d: CardigannDefinition): Values {
		const settings: Record<string, string> = {};
		for (const s of d.settings) {
			if (!valued(s.type) || fileOf(s.name)) continue;
			const stored = editing?.settings?.[s.name];
			if (s.type === "select") {
				settings[s.name] = selectKey(s, stored ?? s.default);
				continue;
			}
			settings[s.name] = stored ?? (s.secret ? "" : (s.default ?? ""));
		}
		return {
			name: editing?.name ?? d.name,
			site: editing?.site ?? d.links[0] ?? "",
			enabled: editing?.enabled ?? true,
			priority: editing?.priority ?? 25,
			private: editing?.private ?? d.type !== "public",
			proxy: editing?.proxy ?? "",
			settings,
		};
	}

	const qc = useQueryClient();

	function body(v: Values) {
		const out: Record<string, unknown> = {
			name: v.name,
			protocol: "cardigann",
			definition,
			enabled: v.enabled,
			priority: v.priority,
			private: v.private,
			settings: v.settings,
		};
		// Always named, so picking the first link back on an edit reaches
		// the server: a `site` left out means "keep the stored one".
		if (v.site) out.site = v.site;
		// The stored proxy comes back without its credentials: sending that
		// form back unchanged would drop them.
		if (!editing || v.proxy !== (editing.proxy ?? "")) out.proxy = v.proxy;
		return out;
	}

	const save = createMutation<Indexer, Error, Values>(() => ({
		mutationFn: (v) =>
			editing
				? api<Indexer>(`/indexers/${encodeURIComponent(editing.name)}`, {
						method: "PUT",
						body: body(v),
					})
				: api<Indexer>("/indexers", { method: "POST", body: body(v) }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok(editing ? i18n.indexer_updated() : i18n.indexer_added());
			onClose();
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: {
			name: "",
			site: "",
			enabled: true,
			priority: 25,
			private: false,
			proxy: "",
			settings: {},
		} as Values,
		validators: { onChange: cardigannIndexerForm },
		onSubmit: ({ value }) => {
			if (!drafting) {
				save.mutate(value);
				return;
			}
			const defaults = def.data ? seed(def.data).settings : {};
			yaml = indexerYaml({
				name: value.name,
				enabled: value.enabled,
				priority: value.priority,
				private: value.private,
				definition: definition ?? "",
				commit: updates.data?.upstream.commit ?? "",
				site: value.site === def.data?.links[0] ? "" : value.site,
				proxy: value.proxy,
				settings: Object.fromEntries(
					Object.entries(value.settings).filter(([k, v]) => v !== defaults[k]),
				),
			});
		},
	}));

	$effect(() => {
		if (def.data && open) form.reset(seed(def.data));
	});

	let secretsSet = $derived(new Set(editing?.secrets_set ?? []));
</script>

<ConfigFormShell
	{open}
	title={editing
		? config.readOnly
			? i18n.indexer_view()
			: i18n.indexer_edit()
		: i18n.cardigann_add_title({ name: def.data?.name ?? "" })}
	size="xl"
	formId="cardigann-form"
	showSubmit={drafting || undefined}
	submitLabel={drafting
		? i18n.cardigann_show_yaml()
		: form.state.isSubmitting
			? i18n.common_saving()
			: editing
				? i18n.common_save_changes()
				: i18n.indexer_add()}
	submitDisabled={!def.data ||
		!form.state.canSubmit ||
		form.state.isSubmitting ||
		(drafting && !updates.data?.upstream.commit)}
	{onClose}
>
	<form
		id="cardigann-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		{#if def.isPending}
			<p class="text-sm text-fg-muted">{i18n.common_loading()}</p>
		{:else if def.isError}
			<p class="text-sm text-status-failed">{errorText(def.error)}</p>
		{:else if def.data}
			{@const d = def.data}
			{#snippet fields()}
				<div class="space-y-5">
					<div class="rounded-md border border-border bg-bg-card px-3 py-2">
						<div class="text-xs font-semibold text-fg">{d.name}</div>
						<div class="text-[11px] text-fg-subtle">{d.description}</div>
					</div>
					{#if d.captcha}
						<p class="flex gap-2 rounded-md bg-status-wanted/10 px-3 py-2 text-xs text-fg">
							<TriangleAlert size={14} class="mt-0.5 shrink-0 text-status-wanted" aria-hidden="true" />
							{i18n.cardigann_captcha_hint()}
						</p>
					{/if}
					{#if d.challenge}
						<p class="flex gap-2 rounded-md bg-status-wanted/10 px-3 py-2 text-xs text-fg">
							<TriangleAlert size={14} class="mt-0.5 shrink-0 text-status-wanted" aria-hidden="true" />
							{i18n.cardigann_challenge_hint()}
						</p>
					{/if}

					<div class="flex flex-wrap items-end gap-3">
						<div class="min-w-0 flex-1">
							<form.Field name="name">
								{#snippet children(field)}
									<TextField {field} label={i18n.common_name()} readonly={editing !== null} />
								{/snippet}
							</form.Field>
						</div>
						<form.Field name="enabled">
							{#snippet children(field)}
								<TogglePill
									label={i18n.common_enabled()}
									tone="status"
									name={field.name}
									checked={field.state.value}
									onChange={(v) => field.handleChange(v)}
								/>
							{/snippet}
						</form.Field>
						<form.Field name="private">
							{#snippet children(field)}
								<TogglePill
									label={field.state.value ? i18n.releases_private() : i18n.releases_public()}
									name={field.name}
									checked={field.state.value}
									onChange={(v) => field.handleChange(v)}
								/>
							{/snippet}
						</form.Field>
					</div>

					{#if d.links.length > 1}
						<form.Field name="site">
							{#snippet children(field)}
								<Select
									label={i18n.cardigann_site()}
									value={field.state.value}
									onChange={(v) => field.handleChange(v)}
									options={d.links.map((l) => ({ value: l, label: l }))}
								/>
							{/snippet}
						</form.Field>
					{/if}

					<div class="space-y-4 rounded-lg border border-border bg-bg-card p-5">
						{#each d.settings as s (s.name)}
							{#if valued(s.type) && fileOf(s.name)}
								<div class="text-sm">
									<div class="font-medium text-fg">{s.label}</div>
									<div class="text-xs text-fg-muted">
										{i18n.cardigann_setting_file({ path: fileOf(s.name) ?? "" })}
									</div>
								</div>
							{:else if s.type === "text" || s.type === "password"}
								<form.Field name={`settings.${s.name}`}>
									{#snippet children(field)}
										<TextField
											{field}
											label={s.label}
											type={s.secret ? "password" : "text"}
											autocomplete="off"
											help={s.secret && secretsSet.has(s.name)
												? i18n.cardigann_secret_keep()
												: undefined}
										/>
									{/snippet}
								</form.Field>
							{:else if s.type === "checkbox"}
								<form.Field name={`settings.${s.name}`}>
									{#snippet children(field)}
										<TogglePill
											label={s.label}
											name={field.name}
											checked={field.state.value === "true"}
											onChange={(v) => field.handleChange(v ? "true" : "false")}
										/>
									{/snippet}
								</form.Field>
							{:else if s.type === "select"}
								<form.Field name={`settings.${s.name}`}>
									{#snippet children(field)}
										<Select
											label={s.label}
											value={field.state.value ?? ""}
											onChange={(v) => field.handleChange(v)}
											options={(s.options ?? []).map((o) => ({ value: o.key, label: o.label }))}
										/>
									{/snippet}
								</form.Field>
							{:else if s.default}
								<p class="flex gap-2 text-xs text-fg-muted">
									<Info size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
									<span class="whitespace-pre-line">{s.default}</span>
								</p>
							{/if}
						{/each}
					</div>

					<form.Field name="proxy">
						{#snippet children(field)}
							<TextField
								{field}
								label={i18n.cardigann_proxy()}
								placeholder="socks5://vpn:1080"
								help={i18n.cardigann_proxy_help()}
							/>
						{/snippet}
					</form.Field>

					<form.Field name="priority">
						{#snippet children(field)}
							<TextField {field} label={i18n.common_priority()} type="number" min={0} max={255} />
						{/snippet}
					</form.Field>
				</div>
			{/snippet}
			{#if drafting}
				<fieldset class="min-w-0">{@render fields()}</fieldset>
			{:else}
				<ReadOnlyFieldset>{@render fields()}</ReadOnlyFieldset>
			{/if}
		{/if}
	</form>

	{#snippet test(variant)}
		{#if editing}
			<TestConnectionButton
				endpoint="/indexers/{encodeURIComponent(editing.name)}/test"
				size="md"
				{variant}
			/>
		{:else}
			<TestConnectionButton
				endpoint="/indexers/test"
				body={() => body(form.state.values)}
				size="md"
				{variant}
			/>
		{/if}
	{/snippet}
</ConfigFormShell>

<CardigannYamlModal
	title={i18n.cardigann_add_title({ name: def.data?.name ?? "" })}
	{yaml}
	onClose={() => (yaml = null)}
/>
