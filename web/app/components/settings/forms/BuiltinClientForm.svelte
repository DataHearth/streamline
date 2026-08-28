<script lang="ts">
	import { Zap, Folder, Gauge, Globe, TriangleAlert } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";
	import type { AppForm } from "../../../lib/form";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	type Values = {
		download_dir: string;
		bind_interface: string;
		listen_port: number;
		max_download_kbps: number;
		max_upload_kbps: number;
		seed_ratio: number;
		seed_time: string;
		disable_dht: boolean;
		enabled: boolean;
	};

	type Props = {
		form: AppForm<Values>;
		isEdit?: boolean;
		// The effective port when the top-level torrent_listen_port overrides
		// this entry's own. Editing listen_port then has no effect, so the field
		// goes read-only and says why instead of accepting a value the engine
		// will ignore.
		listenPortOverride?: number | undefined;
	};

	let { form, isEdit = false, listenPortOverride }: Props = $props();

	let overridden = $derived(listenPortOverride !== undefined);

	// Common network interfaces to bind to. Empty = all interfaces.
	const INTERFACES = [
		{ value: "", label: i18n.builtin_all_interfaces() },
		{ value: "wg0", label: "wg0 · WireGuard (VPN)" },
		{ value: "tun0", label: "tun0 · OpenVPN (VPN)" },
		{ value: "tailscale0", label: "tailscale0 · Tailscale" },
		{ value: "eth0", label: "eth0 · Ethernet" },
		{ value: "eth1", label: "eth1 · Ethernet" },
	];
</script>

<div class="space-y-5">
	<!-- Identity: the built-in engine has no name/host — just an enabled state. -->
	<div
		class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-accent-line bg-accent-soft/40 p-4"
	>
		<div class="flex items-center gap-3">
			<div
				class="grid h-11 w-11 shrink-0 place-items-center rounded-md bg-accent-soft text-accent"
			>
				<Zap size={22} aria-hidden="true" />
			</div>
			<div class="min-w-0">
				<div class="text-sm font-semibold text-fg">{i18n.builtin_engine()}</div>
				<div class="mt-0.5 text-xs text-fg-muted">
					{i18n.builtin_engine_help()}
				</div>
			</div>
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

	<form.Field name="download_dir">
		{#snippet children(field)}
			<TextField
				{field}
				label={i18n.builtin_download_dir()}
				placeholder="/data/torrents"
				help={i18n.builtin_download_dir_help()}
			/>
		{/snippet}
	</form.Field>

	<!-- Network -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Globe size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>{i18n.builtin_network()}</span
			>
		</div>
		<form.Field name="bind_interface">
			{#snippet children(field)}
				{@const opts = INTERFACES.some((o) => o.value === field.state.value)
					? INTERFACES
					: [
							...INTERFACES,
							{ value: field.state.value, label: field.state.value },
						]}
				<div class="block">
					<Select
						label={i18n.builtin_bind_interface()}
						value={field.state.value}
						options={opts}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.builtin_bind_help()}
					</p>
				</div>
			{/snippet}
		</form.Field>
		<div class="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-start">
			<form.Field name="listen_port">
				{#snippet children(field)}
					<div>
						<TextField
							{field}
							label={i18n.builtin_listen_port()}
							type="number"
							min={0}
							max={65535}
							readonly={overridden}
							help={overridden
								? i18n.builtin_listen_port_overridden_help()
								: i18n.builtin_listen_port_help()}
						/>
						{#if overridden}
							<p
								class="mt-2 flex gap-2 text-xs leading-relaxed text-status-wanted"
							>
								<TriangleAlert
									size={14}
									class="mt-px shrink-0"
									aria-hidden="true"
								/>
								<span>
									{i18n.builtin_listen_port_overridden({
										port: listenPortOverride ?? 0,
									})}
								</span>
							</p>
						{/if}
					</div>
				{/snippet}
			</form.Field>
			<form.Field name="disable_dht">
				{#snippet children(field)}
					<div class="pt-6">
						<TogglePill
							label={i18n.builtin_dht()}
							name={field.name}
							checked={!field.state.value}
							onChange={(v) => field.handleChange(!v)}
						/>
					</div>
				{/snippet}
			</form.Field>
		</div>
		<!-- The tip tells you to set the listen port to your forwarded one, which
		     is the opposite of what the override warning says. Under an override
		     the port is already being supplied that way. -->
		{#if !overridden}
			<p class="text-[11px] text-fg-subtle">
				{i18n.builtin_vpn_tip()}
			</p>
		{/if}
	</div>

	<!-- Speed limits (engine-global) -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Gauge size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>{i18n.builtin_speed_limits()}</span
			>
			<span
				class="rounded-full bg-surface px-2 py-0.5 text-[10px] font-medium text-fg-subtle"
				>engine-global</span
			>
		</div>
		<div class="grid gap-3 sm:grid-cols-2">
			<form.Field name="max_download_kbps">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.builtin_max_download()}
						type="number"
						min={0}
						help={i18n.builtin_unlimited_help()}
					/>
				{/snippet}
			</form.Field>
			<form.Field name="max_upload_kbps">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.builtin_max_upload()}
						type="number"
						min={0}
						help={i18n.builtin_unlimited_help()}
					/>
				{/snippet}
			</form.Field>
		</div>
		<p class="text-[11px] text-fg-subtle">
			{i18n.builtin_speed_limits_help()}
		</p>
	</div>

	<!-- Seeding -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Folder size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>{i18n.status_seeding()}</span
			>
		</div>
		<div class="grid gap-3 sm:grid-cols-2">
			<form.Field name="seed_ratio">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.builtin_seed_ratio()}
						type="number"
						min={0}
						help={i18n.builtin_seed_ratio_help()}
					/>
				{/snippet}
			</form.Field>
			<form.Field name="seed_time">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.builtin_seed_time()}
						placeholder="72h"
						help={i18n.builtin_seed_time_help()}
					/>
				{/snippet}
			</form.Field>
		</div>
	</div>
</div>
