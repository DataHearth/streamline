<script lang="ts">
	import { untrack } from "svelte";
	import { Lock } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import { fieldErrorMessages } from "../../../lib/fieldErrors";
	import TogglePill from "../../forms/TogglePill.svelte";
	import TypePicker from "../../forms/TypePicker.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import { readOnlyLock } from "../../../lib/config.svelte";
	import type { AppForm } from "../../../lib/form";
	import type { DownloadClientType, DownloadClientAuth } from "../../../lib/types";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

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

	const TYPES: { type: DownloadClientType; label: string }[] = [
		{ type: "qbittorrent", label: "qBittorrent" },
		{ type: "transmission", label: "Transmission" },
		{ type: "deluge", label: "Deluge" },
	];

	// No "builtin" entry: the built-in engine has its own form (BuiltinClientForm)
	// and is never a selectable TYPES option here, so client_type can't reach
	// applyPreset as "builtin" — Partial (rather than a dummy entry) says so.
	const PRESETS: Partial<Record<DownloadClientType, { name: string; port: number }>> = {
		qbittorrent: { name: "qBittorrent", port: 8080 },
		transmission: { name: "Transmission", port: 9091 },
		deluge: { name: "Deluge", port: 8112 },
	};

	// Fill name + default port for the chosen type, but only when the field
	// still holds a blank or another preset's value so manual edits survive.
	function applyPreset(t: DownloadClientType) {
		const preset = PRESETS[t];
		if (!preset) return;
		const known = Object.values(PRESETS).filter((p) => p !== undefined);
		const cur = form.state.values;
		const presetNames = new Set(known.map((p) => p.name));
		const presetPorts = new Set(known.map((p) => p.port));
		if (!cur.name || presetNames.has(cur.name)) {
			form.setFieldValue("name", preset.name);
		}
		if (!cur.port || presetPorts.has(cur.port)) {
			form.setFieldValue("port", preset.port);
		}
	}

	const clientType = untrack(() => form.useSelector((s) => s.values.client_type));
	const authMethod = untrack(() => form.useSelector((s) => s.values.auth_method));
</script>

<div class="space-y-5">
	<form.Field name="client_type">
		{#snippet children(field)}
			<TypePicker
				label={i18n.dlclient_type()}
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
						label={i18n.common_name()}
						placeholder="My download client"
					/>
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
					<TextField {field} label={i18n.field_host()} placeholder="download.local" />
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

		{#if clientType.current === "qbittorrent"}
			<form.Field name="auth_method">
				{#snippet children(field)}
					<div class="flex flex-wrap items-center gap-2" role="radiogroup">
						<span
							class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
							>{i18n.dlclient_auth_method()}</span
						>
						{#each [{ v: "password" as const, l: i18n.dlclient_user_pass() }, { v: "api_key" as const, l: i18n.field_api_key() }] as o}
							<label
								class="inline-flex h-9 cursor-pointer items-center rounded-md border border-border bg-bg-card px-3 text-xs font-medium text-fg-muted transition hover:border-border-strong has-[:checked]:border-accent has-[:checked]:bg-accent-soft has-[:checked]:text-fg has-[:disabled]:cursor-not-allowed has-[:disabled]:opacity-60"
							>
								<input
									type="radio"
									name={field.name}
									value={o.v}
									checked={field.state.value === o.v}
									disabled={lock()}
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
							<TextField {field} label={i18n.field_username()} autocomplete="off" />
						{/snippet}
					</form.Field>
					<form.Field name="password">
						{#snippet children(field)}
							<TextField
								{field}
								label={i18n.common_password()}
								type="password"
								autocomplete="new-password"
								help={isEdit
									? i18n.dlclient_password_keep()
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
							label={i18n.field_api_key()}
							type="password"
							autocomplete="off"
							help={isEdit
								? i18n.dlclient_apikey_keep_qbit()
								: i18n.dlclient_qbit_apikey_help()}
						/>
					{/snippet}
				</form.Field>
			{/if}
		{:else}
			<div class="grid gap-3 sm:grid-cols-2">
				<form.Field name="username">
					{#snippet children(field)}
						<TextField {field} label={i18n.field_username()} autocomplete="off" />
					{/snippet}
				</form.Field>
				<form.Field name="password">
					{#snippet children(field)}
						<TextField
							{field}
							label={i18n.common_password()}
							type="password"
							autocomplete="new-password"
							help={isEdit
								? i18n.dlclient_password_keep()
								: undefined}
						/>
					{/snippet}
				</form.Field>
			</div>
		{/if}

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
