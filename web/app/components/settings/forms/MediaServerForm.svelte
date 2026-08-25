<script lang="ts">
	import { untrack } from "svelte";
	import { Search } from "@lucide/svelte";
	import { createMutation } from "@tanstack/svelte-query";
	import TextField from "../../forms/TextField.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";
	import Select from "../../forms/Select.svelte";
	import TypePicker from "../../forms/TypePicker.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import PlexPINFlow from "../PlexPINFlow.svelte";
	import { api, errorText } from "../../../lib/api";
	import { readOnlyLock } from "../../../lib/config.svelte";
	import { toast } from "../../../lib/toast";
	import type { AppForm } from "../../../lib/form";
	import { m as i18n } from "../../../lib/paraglide/messages.js";
	import type {
		MediaServerType,
		MediaServerSection,
	} from "../../../lib/types";

	type Values = {
		name: string;
		server_type: MediaServerType;
		host: string;
		api_key: string;
		library_section: string;
		enabled: boolean;
	};

	type Props = {
		form: AppForm<Values>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

	const lock = readOnlyLock();

	const serverType = untrack(() => form.useSelector((s) => s.values.server_type));
	const apiKey = untrack(() => form.useSelector((s) => s.values.api_key));

	const TYPES: { type: MediaServerType; label: string }[] = [
		{ type: "plex", label: "Plex" },
		{ type: "jellyfin", label: "Jellyfin" },
		{ type: "emby", label: "Emby" },
	];

	const PRESETS: Record<MediaServerType, { name: string; host: string }> = {
		plex: { name: "Plex", host: "https://plex.local:32400" },
		jellyfin: { name: "Jellyfin", host: "http://jellyfin.local:8096" },
		emby: { name: "Emby", host: "http://emby.local:8096" },
	};

	const KEY_HINTS: Record<MediaServerType, string> = {
		plex:
			i18n.mediaserver_plex_token_help(),
		jellyfin:
			i18n.mediaserver_jellyfin_help(),
		emby: i18n.mediaserver_emby_help(),
	};

	function applyPreset(t: MediaServerType) {
		const preset = PRESETS[t];
		const cur = form.state.values;
		const presetNames = new Set(Object.values(PRESETS).map((p) => p.name));
		const presetHosts = new Set(Object.values(PRESETS).map((p) => p.host));
		if (!cur.name || presetNames.has(cur.name)) {
			form.setFieldValue("name", preset.name);
		}
		if (!cur.host || presetHosts.has(cur.host)) {
			form.setFieldValue("host", preset.host);
		}
	}

	let sections = $state<MediaServerSection[]>([]);

	const discover = createMutation<
		{ sections: MediaServerSection[] },
		Error,
		void
	>(() => ({
		mutationFn: () => {
			const v = form.state.values;
			return api<{ sections: MediaServerSection[] }>(
				"/media-servers/discover",
				{
					method: "POST",
					body: {
						server_type: v.server_type,
						host: v.host,
						api_key: v.api_key,
					},
				},
			);
		},
		onSuccess: (resp) => {
			sections = resp.sections ?? [];
			if (sections.length === 0) toast.warn("No sections returned");
		},
		onError: (err) => toast.err(i18n.mediaserver_discover_failed({ error: errorText(err) })),
	}));
</script>

<div class="space-y-5">
	<form.Field name="server_type">
		{#snippet children(field)}
			<TypePicker
				label={i18n.mediaserver_type()}
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
						placeholder="Living room Plex"
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
		<form.Field name="host">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.field_url()}
					placeholder="https://plex.local:32400"
					help={i18n.mediaserver_url_help()}
				/>
			{/snippet}
		</form.Field>

		{#if serverType.current === "plex"}
			<form.Field name="api_key">
				{#snippet children(field)}
					<PlexPINFlow
						token={field.state.value ?? ""}
						onToken={(t) => field.handleChange(t)}
					/>
				{/snippet}
			</form.Field>
		{:else}
			<form.Field name="api_key">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.mediaserver_token()}
						type="password"
						autocomplete="off"
						help={isEdit
							? i18n.mediaserver_token_keep()
							: (KEY_HINTS[serverType.current] ?? "")}
					/>
				{/snippet}
			</form.Field>
		{/if}

		{#if serverType.current === "plex"}
			<div class="space-y-2">
				<form.Field name="library_section">
					{#snippet children(field)}
						{#if sections.length > 0}
							<Select
								label={i18n.mediaserver_library_section()}
								value={field.state.value ?? ""}
								options={[
									{ value: "", label: i18n.mediaserver_pick_section() },
									...sections.map((s: MediaServerSection) => ({
										value: s.key,
										label: `${s.name} — ${s.type}`,
									})),
								]}
								onChange={(v) => field.handleChange(v)}
							/>
						{:else}
							<label class="block">
								<span class="mb-1 block text-sm font-medium text-fg">
									{i18n.mediaserver_library_section()}
								</span>
								<input
									type="text"
									name={field.name}
									value={field.state.value ?? ""}
									oninput={(e) =>
										field.handleChange(
											(e.currentTarget as HTMLInputElement).value,
										)}
									placeholder={i18n.mediaserver_section_help()}
									readonly={lock()}
									class="h-10 w-full rounded-md border border-border bg-bg px-3 text-sm text-fg focus-visible:outline-2 focus-visible:outline-accent read-only:cursor-not-allowed read-only:opacity-70"
								/>
							</label>
						{/if}
					{/snippet}
				</form.Field>
				<div class="flex flex-wrap items-center gap-3">
					<button
						type="button"
						disabled={discover.isPending || !apiKey.current}
						onclick={() => discover.mutate()}
						class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-surface px-3 text-xs font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
					>
						<Search size={13} aria-hidden="true" />
						{#if discover.isPending}
							Discovering sections…
						{:else if sections.length > 0}
							Re-discover sections
						{:else}
							Discover sections
						{/if}
					</button>
					{#if !apiKey.current}
						<span class="font-mono text-[10.5px] text-fg-faint">
							{i18n.mediaserver_plex_signin_first()}
						</span>
					{/if}
				</div>
			</div>
		{/if}
	</div>
</div>
