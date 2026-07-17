<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import { Zap, Folder, Gauge, Globe } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import TogglePill from "../../forms/TogglePill.svelte";

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
		form: FormApi<Values, undefined>;
		isEdit?: boolean;
	};

	let { form, isEdit = false }: Props = $props();

	// Common network interfaces to bind to. Empty = all interfaces.
	const INTERFACES = [
		{ value: "", label: "All interfaces" },
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
				<div class="text-sm font-semibold text-fg">Built-in engine</div>
				<div class="mt-0.5 text-xs text-fg-muted">
					Runs a BitTorrent engine inside Streamline — no external client
					needed.
				</div>
			</div>
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

	<form.Field name="download_dir">
		{#snippet children(field)}
			<TextField
				{field}
				label="Download directory"
				placeholder="/data/torrents"
				help="Absolute path where the engine writes completed and in-progress data."
			/>
		{/snippet}
	</form.Field>

	<!-- Network -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Globe size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>Network</span
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
						label="Bind to interface"
						value={field.state.value}
						options={opts}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						Pin peer traffic to one interface. Choose your VPN
						interface so traffic never leaves the tunnel — if it drops,
						the engine stops instead of leaking over the default route.
					</p>
				</div>
			{/snippet}
		</form.Field>
		<div class="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-start">
			<form.Field name="listen_port">
				{#snippet children(field)}
					<TextField
						{field}
						label="Listen port"
						type="number"
						min={0}
						max={65535}
						help="Incoming peer port. Behind a VPN, set this to the port your provider forwards. 0 = auto-select."
					/>
				{/snippet}
			</form.Field>
			<form.Field name="disable_dht">
				{#snippet children(field)}
					<div class="pt-6">
						<TogglePill
							label="DHT"
							name={field.name}
							checked={!field.state.value}
							onChange={(v) => field.handleChange(!v)}
						/>
					</div>
				{/snippet}
			</form.Field>
		</div>
		<p class="text-[11px] text-fg-subtle">
			VPN + port forwarding: bind to the VPN interface and set the listen
			port to the one your provider forwards.
		</p>
	</div>

	<!-- Speed limits (engine-global) -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Gauge size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>Speed limits</span
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
						label="Max download · KB/s"
						type="number"
						min={0}
						help="0 = unlimited."
					/>
				{/snippet}
			</form.Field>
			<form.Field name="max_upload_kbps">
				{#snippet children(field)}
					<TextField
						{field}
						label="Max upload · KB/s"
						type="number"
						min={0}
						help="0 = unlimited."
					/>
				{/snippet}
			</form.Field>
		</div>
		<p class="text-[11px] text-fg-subtle">
			Limits apply to the whole engine, not per-torrent.
		</p>
	</div>

	<!-- Seeding -->
	<div class="rounded-lg border border-border bg-bg-card p-5 space-y-4">
		<div class="flex items-center gap-2">
			<Folder size={13} class="text-fg-faint" aria-hidden="true" />
			<span
				class="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
				>Seeding</span
			>
		</div>
		<div class="grid gap-3 sm:grid-cols-2">
			<form.Field name="seed_ratio">
				{#snippet children(field)}
					<TextField
						{field}
						label="Seed ratio"
						type="number"
						min={0}
						help="Stop seeding at this ratio. 0 = unlimited."
					/>
				{/snippet}
			</form.Field>
			<form.Field name="seed_time">
				{#snippet children(field)}
					<TextField
						{field}
						label="Seed time"
						placeholder="72h"
						help="Go duration. Empty = unlimited."
					/>
				{/snippet}
			</form.Field>
		</div>
	</div>
</div>
