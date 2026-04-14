<script lang="ts">
	import { untrack } from "svelte";
	import type { FormApi } from "@tanstack/svelte-form";
	import { Search } from "@lucide/svelte";
	import { createMutation } from "@tanstack/svelte-query";
	import TextField from "../../forms/TextField.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";
	import Select from "../../forms/Select.svelte";
	import TypePicker from "../../forms/TypePicker.svelte";
	import BrandLogo from "../BrandLogo.svelte";
	import PlexPINFlow from "../PlexPINFlow.svelte";
	import { api } from "../../../lib/api";
	import { toast } from "../../../lib/toast";
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
		form: FormApi<Values, undefined>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

	const serverType = untrack(() => form.useStore((s) => s.values.server_type));
	const apiKey = untrack(() => form.useStore((s) => s.values.api_key));

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
			"Click Connect with Plex above. The token is filled in automatically after you sign in.",
		jellyfin:
			"Generate from your Jellyfin Dashboard → Advanced → API Keys.",
		emby: "Generate from your Emby Dashboard → Advanced → API Keys.",
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
		onError: (err) => toast.err(`Discover failed: ${err.message}`),
	}));
</script>

<div class="space-y-5">
	<form.Field name="server_type">
		{#snippet children(field)}
			<TypePicker
				label="Server type"
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
						placeholder="Living room Plex"
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
		<form.Field name="host">
			{#snippet children(field)}
				<TextField
					{field}
					label="URL"
					placeholder="https://plex.local:32400"
					help="Full base URL including scheme and port."
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
						label="Token / API key"
						type="password"
						autocomplete="off"
						help={isEdit
							? "Leave blank to keep the existing token/API key."
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
								label="Library section"
								value={field.state.value ?? ""}
								options={[
									{ value: "", label: "Pick a section" },
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
									Library section
								</span>
								<input
									type="text"
									name={field.name}
									value={field.state.value ?? ""}
									oninput={(e) =>
										field.handleChange(
											(e.currentTarget as HTMLInputElement).value,
										)}
									placeholder="Click Discover to enumerate sections, or enter a section key"
									class="h-10 w-full rounded-md border border-border bg-bg px-3 text-sm text-fg focus-visible:outline-2 focus-visible:outline-accent"
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
							Sign in with Plex first
						</span>
					{/if}
				</div>
			</div>
		{/if}
	</div>
</div>
