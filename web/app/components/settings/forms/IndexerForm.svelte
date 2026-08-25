<script lang="ts">
	import { untrack } from "svelte";
	import { Lock, Rss } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import { fieldErrorMessages } from "../../../lib/fieldErrors";
	import TogglePill from "../../forms/TogglePill.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import { cn } from "../../../lib/cn";
	import { readOnlyLock } from "../../../lib/config.svelte";
	import type { AppForm } from "../../../lib/form";
	import type { IndexerProtocol } from "../../../lib/types";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	type Values = {
		name: string;
		protocol: IndexerProtocol;
		host: string;
		port: number;
		path: string;
		use_ssl: boolean;
		api_key: string;
		// Undefined (not 0) while the field is cleared: Number("") is 0, a value
		// the schema accepts, so clearing the input has to produce something the
		// "Priority required" check still rejects.
		priority: number | undefined;
		enabled: boolean;
	};

	type Props = {
		form: AppForm<Values>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

	const lock = readOnlyLock();

	type Preset = {
		slug: string;
		label: string;
		protocol: IndexerProtocol;
		name?: string;
		host?: string;
		port?: number;
		path: string;
	};

	// Aggregators query every indexer they manage in a single request: Prowlarr
	// via its native JSON API, Jackett via the standard /indexers/all Torznab
	// feed (which is why Jackett stays protocol=torznab).
	const AGGREGATORS: Preset[] = [
		{
			slug: "prowlarr",
			label: "Prowlarr",
			protocol: "prowlarr",
			name: "Prowlarr",
			host: "prowlarr.local",
			port: 9696,
			path: "",
		},
		{
			slug: "jackett",
			label: "Jackett",
			protocol: "torznab",
			name: "Jackett",
			host: "jackett.local",
			port: 9117,
			path: "/api/v2.0/indexers/all/results/torznab/api",
		},
	];

	// Single Torznab feeds — one tracker endpoint per entry.
	const SINGLE: Preset[] = [
		{ slug: "torznab", label: "Torznab", protocol: "torznab", path: "/api" },
	];

	function applyPreset(p: Preset) {
		form.setFieldValue("protocol", p.protocol);
		form.setFieldValue("path", p.path);
		// Generic Torznab carries no name/host/port — leave whatever the user
		// has typed rather than blanking it.
		if (p.name) form.setFieldValue("name", p.name);
		if (p.host) form.setFieldValue("host", p.host);
		if (p.port) form.setFieldValue("port", p.port);
	}

	const protocol = untrack(() => form.useSelector((s) => s.values.protocol));

	const PROTOCOL_META: Record<
		IndexerProtocol,
		{ label: string; desc: string; logo?: string }
	> = {
		prowlarr: {
			label: "Prowlarr",
			desc: i18n.indexer_native_api(),
			logo: "prowlarr",
		},
		torznab: { label: "Torznab", desc: i18n.indexer_single_endpoint() },
	};

	const meta = $derived(PROTOCOL_META[protocol.current]);
</script>

{#snippet presetChip(p: Preset, aggregator: boolean)}
	<button
		type="button"
		title="Prefill from {p.label}"
		disabled={lock()}
		onclick={() => applyPreset(p)}
		class={cn(
			"inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-border bg-bg-card px-2.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-not-allowed disabled:opacity-60",
			aggregator && "border-accent/30",
		)}
	>
		{#if p.slug === "torznab"}
			<Rss size={16} aria-hidden="true" />
		{:else}
			<BrandLogo name={p.slug} size={16} />
		{/if}
		<span>{p.label}</span>
	</button>
{/snippet}

<div class="space-y-5">
	<div
		class="flex items-center gap-2.5 rounded-md border border-border bg-bg-card px-3 py-2"
	>
		{#if meta.logo}
			<BrandLogo name={meta.logo} size={18} ariaLabel={meta.label} />
		{:else}
			<Rss size={16} class="text-fg-muted" aria-hidden="true" />
		{/if}
		<div class="min-w-0">
			<div class="text-xs font-semibold text-fg">{meta.label}</div>
			<div class="text-[11px] text-fg-subtle">{meta.desc}</div>
		</div>
	</div>

	{#if !isEdit}
		<div class="space-y-3 rounded-lg border border-border bg-bg-deep/40 p-4">
			<div>
				<div class="mb-1.5 flex flex-wrap items-center gap-2">
					<span
						class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
						>{i18n.indexer_aggregators()}</span
					>
					<span class="text-[11px] text-fg-subtle"
						>query every indexer at once</span
					>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each AGGREGATORS as p (p.slug)}
						{@render presetChip(p, true)}
					{/each}
				</div>
			</div>
			<div class="border-t border-border"></div>
			<div>
				<div class="mb-1.5 flex flex-wrap items-center gap-2">
					<span
						class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
						>{i18n.indexer_single_feed()}</span
					>
					<span class="text-[11px] text-fg-subtle"
						>one tracker per entry</span
					>
				</div>
				<div class="flex flex-wrap gap-2">
					{#each SINGLE as p (p.slug)}
						{@render presetChip(p, false)}
					{/each}
				</div>
			</div>
		</div>
	{/if}

	<div class="flex flex-wrap items-end gap-3">
		<div class="min-w-0 flex-1">
			<form.Field name="name">
				{#snippet children(field)}
					<TextField {field} label={i18n.common_name()} placeholder="My indexer" />
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
	</div>

	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="grid gap-3 sm:grid-cols-[1fr_6rem_auto] sm:items-end">
			<form.Field name="host">
				{#snippet children(field)}
					<TextField {field} label={i18n.field_host()} placeholder="prowlarr.local" />
				{/snippet}
			</form.Field>
			<form.Field name="port">
				{#snippet children(field)}
					<TextField {field} label={i18n.field_port()} type="number" min={1} max={65535} />
				{/snippet}
			</form.Field>
			<form.Field name="use_ssl">
				{#snippet children(field)}
					<TogglePill
						label={i18n.field_https()}
						icon={Lock}
						name={field.name}
						checked={field.state.value}
						onChange={(v) => field.handleChange(v)}
					/>
				{/snippet}
			</form.Field>
		</div>

		<div class="grid gap-3 sm:grid-cols-2">
			<form.Field name="path">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.field_path()}
						placeholder={protocol.current === "prowlarr" ? "(blank)" : "/api"}
						help={protocol.current === "prowlarr"
							? i18n.indexer_urlbase_help()
							: i18n.indexer_path_help()}
					/>
				{/snippet}
			</form.Field>
			<form.Field name="api_key">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.field_api_key()}
						type="password"
						autocomplete="off"
						help={isEdit
							? i18n.indexer_apikey_keep()
							: undefined}
					/>
				{/snippet}
			</form.Field>
		</div>

		<div class="border-t border-border"></div>

		<form.Field name="priority">
			{#snippet children(field)}
				{@const errors = fieldErrorMessages(field)}
				<div>
					<label
						class="flex items-center gap-1.5 text-xs font-medium text-fg-muted"
					>
						{i18n.common_priority()}
						<input
							type="number"
							inputmode="numeric"
							min="0"
							max="255"
							name={field.name}
							value={field.state.value ?? ""}
							oninput={(e) => {
								const raw = (e.currentTarget as HTMLInputElement).value;
								// Number("") is 0 — a priority the schema accepts, so a
								// cleared field would save silently. undefined keeps the
								// input empty and lets validation report it as missing.
								field.handleChange(raw === "" ? undefined : Number(raw));
							}}
							onblur={() => field.handleBlur()}
							readonly={lock()}
							class="h-9 w-16 rounded-md border bg-bg-elevated px-2 text-center text-sm text-fg focus-visible:outline-2 focus-visible:outline-accent read-only:cursor-not-allowed read-only:opacity-70"
							class:border-status-failed={errors.length > 0}
							class:border-border={errors.length === 0}
						/>
					</label>
					{#each errors as msg}
						<p class="mt-1 text-xs text-status-failed">{msg}</p>
					{/each}
				</div>
			{/snippet}
		</form.Field>
	</div>
</div>
