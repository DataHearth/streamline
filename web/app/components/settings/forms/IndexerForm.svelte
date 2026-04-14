<script lang="ts">
	import { untrack } from "svelte";
	import type { FormApi } from "@tanstack/svelte-form";
	import { Lock, Rss } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import { cn } from "../../../lib/cn";
	import type { IndexerProtocol } from "../../../lib/types";

	type Values = {
		name: string;
		protocol: IndexerProtocol;
		host: string;
		port: number;
		path: string;
		use_ssl: boolean;
		api_key: string;
		priority: number;
		enabled: boolean;
	};

	type Props = {
		form: FormApi<Values, undefined>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

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

	const protocol = untrack(() => form.useStore((s) => s.values.protocol));

	const PROTOCOL_META: Record<
		IndexerProtocol,
		{ label: string; desc: string; logo?: string }
	> = {
		prowlarr: {
			label: "Prowlarr",
			desc: "Native API · queries all indexers",
			logo: "prowlarr",
		},
		torznab: { label: "Torznab", desc: "Single feed endpoint" },
	};

	const meta = $derived(PROTOCOL_META[protocol.current]);
</script>

{#snippet presetChip(p: Preset, aggregator: boolean)}
	<button
		type="button"
		title="Prefill from {p.label}"
		onclick={() => applyPreset(p)}
		class={cn(
			"inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-border bg-bg-card px-2.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg",
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
						>Aggregators</span
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
						>Single Torznab feed</span
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
					<TextField {field} label="Name" placeholder="My indexer" />
				{/snippet}
			</form.Field>
		</div>
		<form.Field name="enabled">
			{#snippet children(field)}
				<TogglePill
					label="Enabled"
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
					<TextField {field} label="Host" placeholder="prowlarr.local" />
				{/snippet}
			</form.Field>
			<form.Field name="port">
				{#snippet children(field)}
					<TextField {field} label="Port" type="number" min={1} max={65535} />
				{/snippet}
			</form.Field>
			<form.Field name="use_ssl">
				{#snippet children(field)}
					<TogglePill
						label="HTTPS"
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
						label="Path"
						placeholder={protocol.current === "prowlarr" ? "(blank)" : "/api"}
						help={protocol.current === "prowlarr"
							? "Only set this if Prowlarr runs under a URL base; the /api/v1 path is added automatically."
							: "Torznab API path, e.g. /api (Jackett uses its /indexers/all/…/torznab/api endpoint)."}
					/>
				{/snippet}
			</form.Field>
			<form.Field name="api_key">
				{#snippet children(field)}
					<TextField
						{field}
						label="API key"
						type="password"
						autocomplete="off"
						help={isEdit
							? "Leave blank to keep the existing API key."
							: undefined}
					/>
				{/snippet}
			</form.Field>
		</div>

		<div class="border-t border-border"></div>

		<form.Field name="priority">
			{#snippet children(field)}
				<label class="flex items-center gap-1.5 text-xs font-medium text-fg-muted">
					Priority
					<input
						type="number"
						inputmode="numeric"
						min="0"
						max="255"
						name={field.name}
						value={field.state.value}
						oninput={(e) =>
							field.handleChange(
								Number((e.currentTarget as HTMLInputElement).value),
							)}
						class="h-9 w-16 rounded-md border border-border bg-bg-elevated px-2 text-center text-sm text-fg focus-visible:outline-2 focus-visible:outline-accent"
					/>
				</label>
			{/snippet}
		</form.Field>
	</div>
</div>
