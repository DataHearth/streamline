<script lang="ts">
	import { untrack } from "svelte";
	import type { FormApi } from "@tanstack/svelte-form";
	import { Lock } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";
	import TypePicker from "../../forms/TypePicker.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import type { DownloadClientType, DownloadClientAuth } from "../../../lib/types";

	type Values = {
		name: string;
		client_type: DownloadClientType;
		host: string;
		port: number;
		auth_method: DownloadClientAuth;
		username: string;
		password: string;
		api_key: string;
		use_ssl: boolean;
		priority: number;
		enabled: boolean;
	};

	type Props = {
		form: FormApi<Values, undefined>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

	const TYPES: { type: DownloadClientType; label: string }[] = [
		{ type: "qbittorrent", label: "qBittorrent" },
		{ type: "transmission", label: "Transmission" },
		{ type: "deluge", label: "Deluge" },
	];

	const PRESETS: Record<DownloadClientType, { name: string; port: number }> = {
		qbittorrent: { name: "qBittorrent", port: 8080 },
		transmission: { name: "Transmission", port: 9091 },
		deluge: { name: "Deluge", port: 8112 },
	};

	// Fill name + default port for the chosen type, but only when the field
	// still holds a blank or another preset's value so manual edits survive.
	function applyPreset(t: DownloadClientType) {
		const preset = PRESETS[t];
		const cur = form.state.values;
		const presetNames = new Set(Object.values(PRESETS).map((p) => p.name));
		const presetPorts = new Set(Object.values(PRESETS).map((p) => p.port));
		if (!cur.name || presetNames.has(cur.name)) {
			form.setFieldValue("name", preset.name);
		}
		if (!cur.port || presetPorts.has(cur.port)) {
			form.setFieldValue("port", preset.port);
		}
	}

	const clientType = untrack(() => form.useStore((s) => s.values.client_type));
	const authMethod = untrack(() => form.useStore((s) => s.values.auth_method));
</script>

<div class="space-y-5">
	<form.Field name="client_type">
		{#snippet children(field)}
			<TypePicker
				label="Client type"
				name={field.name}
				value={field.state.value}
				locked={isEdit}
				lockedHint="Type can't be changed once selected."
				options={TYPES.map((t) => ({ value: t.type, label: t.label }))}
				onChange={(v) => {
					field.handleChange(v);
					applyPreset(v);
				}}
			>
				{#snippet logo(v)}
					<BrandLogo name={v} size={20} />
				{/snippet}
			</TypePicker>
		{/snippet}
	</form.Field>

	<div class="flex flex-wrap items-end gap-3">
		<div class="min-w-0 flex-1">
			<form.Field name="name">
				{#snippet children(field)}
					<TextField
						{field}
						label="Name"
						placeholder="My download client"
					/>
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
					<TextField {field} label="Host" placeholder="download.local" />
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

		{#if clientType.current === "qbittorrent"}
			<form.Field name="auth_method">
				{#snippet children(field)}
					<div class="flex flex-wrap items-center gap-2" role="radiogroup">
						<span
							class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
							>Auth method</span
						>
						{#each [{ v: "password" as const, l: "Username & password" }, { v: "api_key" as const, l: "API key" }] as o}
							<label
								class="inline-flex h-9 cursor-pointer items-center rounded-md border border-border bg-bg-card px-3 text-xs font-medium text-fg-muted transition hover:border-border-strong has-[:checked]:border-accent has-[:checked]:bg-accent-soft has-[:checked]:text-fg"
							>
								<input
									type="radio"
									name={field.name}
									value={o.v}
									checked={field.state.value === o.v}
									onchange={() => field.handleChange(o.v)}
									class="sr-only"
								/>
								{o.l}
							</label>
						{/each}
					</div>
				{/snippet}
			</form.Field>

			{#if authMethod.current === "password"}
				<div class="grid gap-3 sm:grid-cols-2">
					<form.Field name="username">
						{#snippet children(field)}
							<TextField {field} label="Username" autocomplete="off" />
						{/snippet}
					</form.Field>
					<form.Field name="password">
						{#snippet children(field)}
							<TextField
								{field}
								label="Password"
								type="password"
								autocomplete="new-password"
								help={isEdit
									? "Leave blank to keep the existing password."
									: undefined}
							/>
						{/snippet}
					</form.Field>
				</div>
			{:else}
				<form.Field name="api_key">
					{#snippet children(field)}
						<TextField
							{field}
							label="API key"
							type="password"
							autocomplete="off"
							help={isEdit
								? "Leave blank to keep the existing API key. Generate in qBittorrent → Preferences → WebUI → API Key. Requires qBittorrent 5.2.0+."
								: "Generate in qBittorrent → Preferences → WebUI → API Key. Requires qBittorrent 5.2.0+."}
						/>
					{/snippet}
				</form.Field>
			{/if}
		{:else}
			<div class="grid gap-3 sm:grid-cols-2">
				<form.Field name="username">
					{#snippet children(field)}
						<TextField {field} label="Username" autocomplete="off" />
					{/snippet}
				</form.Field>
				<form.Field name="password">
					{#snippet children(field)}
						<TextField
							{field}
							label="Password"
							type="password"
							autocomplete="new-password"
							help={isEdit
								? "Leave blank to keep the existing password."
								: undefined}
						/>
					{/snippet}
				</form.Field>
			</div>
		{/if}

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
