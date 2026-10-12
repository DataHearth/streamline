<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Info, TriangleAlert } from "@lucide/svelte";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import ReadOnlyFieldset from "@components/settings/ReadOnlyFieldset.svelte";
	import TestConnectionButton from "@components/settings/TestConnectionButton.svelte";
	import TextField from "@components/forms/TextField.svelte";
	import TogglePill from "@components/forms/TogglePill.svelte";
	import Select from "@components/forms/Select.svelte";
	import { api, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import { cardigannIndexerForm } from "@lib/schemas";
	import type { CardigannDefinition, Indexer } from "@lib/types";
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

	const def = createQuery<CardigannDefinition>(() => ({
		queryKey: ["cardigann", "definition", definition],
		queryFn: () =>
			api<CardigannDefinition>(
				`/cardigann/catalog/${encodeURIComponent(definition ?? "")}`,
			),
		enabled: definition !== null,
	}));

	const valued = (type: string) =>
		type === "text" || type === "password" || type === "checkbox" || type === "select";

	function seed(d: CardigannDefinition): Values {
		const settings: Record<string, string> = {};
		for (const s of d.settings) {
			if (!valued(s.type)) continue;
			settings[s.name] = editing?.settings?.[s.name] ??
				(s.secret ? "" : (s.default ?? (s.type === "select" ? (s.options?.[0]?.key ?? "") : "")));
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
		const first = def.data?.links[0];
		if (v.site && v.site !== first) out.site = v.site;
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
		onSubmit: ({ value }) => save.mutate(value),
	}));

	$effect(() => {
		if (def.data && open) form.reset(seed(def.data));
	});

	let secretsSet = $derived(new Set(editing?.secrets_set ?? []));
</script>

<ConfigFormShell
	{open}
	title={config.readOnly
		? i18n.indexer_view()
		: editing
			? i18n.indexer_edit()
			: i18n.cardigann_add_title({ name: def.data?.name ?? "" })}
	size="xl"
	formId="cardigann-form"
	submitLabel={form.state.isSubmitting
		? i18n.common_saving()
		: editing
			? i18n.common_save_changes()
			: i18n.indexer_add()}
	submitDisabled={!def.data || !form.state.canSubmit || form.state.isSubmitting}
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
			<ReadOnlyFieldset>
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
							{#if s.type === "text" || s.type === "password"}
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
			</ReadOnlyFieldset>
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
