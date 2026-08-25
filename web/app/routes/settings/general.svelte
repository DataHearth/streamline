<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import {
		Play,
		Globe,
		Folder,
		Database,
		Lock,
	} from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import type { DiskUsage, SystemInfo } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const info = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
	}));

	function barClass(kind: DiskUsage["kind"]) {
		if (kind === "err") return "bg-status-failed";
		if (kind === "warn") return "bg-status-wanted";
		return "bg-status-available";
	}
</script>

<header>
	<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_general()}</h1>
	<p class="mt-1 max-w-2xl text-sm text-fg-muted">
		{i18n.settings_general_intro()}
	</p>
	{#if info.data?.ffmpeg_warn}
		<p class="mt-3 flex items-center gap-2">
			<span
				class="inline-flex items-center gap-1.5 rounded-full bg-status-wanted/14 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-wanted"
			>
				{i18n.settings_ffmpeg_missing()}
			</span>
			<span class="text-xs text-fg-muted">{i18n.probe_not_found()}</span>
		</p>
	{/if}
</header>

{#if info.isPending}
	<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
{:else if info.isError}
	<p class="mt-6 text-sm text-status-failed">
		{i18n.err_load_failed_detail({ reason: errorText(info.error) })}
	</p>
{:else if info.data}
	{@const d = info.data}
	<div class="mt-6 grid grid-cols-1 gap-3 md:grid-cols-2">
		{@render card(Play, "App name", d.app_name, false, null)}
		{@render card(
			Globe,
			"Public URL",
			d.public_url,
			true,
			d.https_warn ? { kind: "warn", label: i18n.settings_no_https() } : null,
		)}
		{@render storageCard(
			Folder,
			"Data directory",
			d.data_dir,
			d.data_usage,
			null,
		)}
		{@render storageCard(
			Database,
			"Database",
			d.db_path,
			d.db_usage,
			d.db_size,
		)}
		{@render card(Lock, "Auth mode", d.auth_mode, true, {
			kind: "ok",
			label: i18n.settings_login_required(),
		})}
	</div>

	<section class="mt-6 rounded-lg border border-border bg-bg-elevated">
		<header
			class="flex items-start justify-between border-b border-border px-5 py-3.5"
		>
			<div>
				<h2 class="text-sm font-semibold text-fg">{i18n.settings_build_runtime()}</h2>
				<p class="mt-0.5 text-xs text-fg-muted">
					{i18n.settings_build_help()}
				</p>
			</div>
		</header>
		<dl class="divide-y divide-border text-sm">
			{@render kv("Version", d.version)}
			{@render kv("Go runtime", d.go_version)}
			{@render kv("Platform", d.go_os_arch)}
			{#if d.commit}
				{@render kv("Commit", d.commit)}
			{/if}
			{#if d.built_at}
				{@render kv("Built at", d.built_at)}
			{/if}
		</dl>
	</section>
{/if}

{#snippet card(
	Icon: typeof Play,
	label: string,
	value: string,
	mono: boolean,
	pill: { kind: "ok" | "warn"; label: string } | null,
)}
	<div class="flex gap-3.5 rounded-lg border border-border bg-bg-elevated p-4">
		<div
			class="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-border bg-bg-card text-fg-muted"
		>
			<Icon size={16} aria-hidden="true" />
		</div>
		<div class="min-w-0 flex-1">
			<div class="flex items-center justify-between gap-2">
				<span
					class="font-mono text-[10px] uppercase tracking-[0.14em] text-fg-muted"
					>{label}</span
				>
				{#if pill}
					<span
						class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide {pill.kind ===
						'ok'
							? 'bg-status-available/14 text-status-available'
							: 'bg-status-wanted/14 text-status-wanted'}"
					>
						{pill.label}
					</span>
				{/if}
			</div>
			<div
				class="mt-1.5 truncate text-sm text-fg"
				class:font-mono={mono}
			>
				{value}
			</div>
		</div>
	</div>
{/snippet}

{#snippet storageCard(
	Icon: typeof Folder,
	label: string,
	value: string,
	usage: DiskUsage | undefined,
	meta: string | undefined | null,
)}
	<div class="flex gap-3.5 rounded-lg border border-border bg-bg-elevated p-4">
		<div
			class="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-border bg-bg-card text-fg-muted"
		>
			<Icon size={16} aria-hidden="true" />
		</div>
		<div class="min-w-0 flex-1">
			<div class="flex items-center justify-between gap-2">
				<span
					class="font-mono text-[10px] uppercase tracking-[0.14em] text-fg-muted"
					>{label}</span
				>
				{#if usage}
					<span
						class="rounded-full bg-status-available/14 px-2 py-0.5 font-mono text-[10px] font-semibold text-status-available"
					>
						{usage.used} · {usage.pct}%
					</span>
				{/if}
			</div>
			<div class="mt-1.5 truncate font-mono text-sm text-fg">{value}</div>
			{#if usage}
				<div class="mt-2 h-1 overflow-hidden rounded-full bg-bg-card">
					<div
						class="h-full rounded-full {barClass(usage.kind)}"
						style:width="{usage.pct}%"
					></div>
				</div>
				<div class="mt-1.5 text-[11px] text-fg-subtle">
					{usage.free} free of {usage.total}{#if meta} · {meta}{/if}
				</div>
			{:else if meta}
				<div class="mt-1 text-[11px] text-fg-subtle">{meta}</div>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet kv(label: string, value: string)}
	<!-- grid-cols-[160px_1fr] was 45% of a 358px content width, leaving 182px for
	     values like `go1.24.1 linux/amd64` or a commit sha. Below sm the label
	     goes above the value and the value gets the whole line. -->
	<div class="px-5 py-3 sm:grid sm:grid-cols-[160px_1fr] sm:items-center sm:gap-4">
		<dt class="text-xs font-medium text-fg-muted">{label}</dt>
		<dd class="mt-1 break-all font-mono text-sm text-fg sm:mt-0 sm:break-normal">
			{value}
		</dd>
	</div>
{/snippet}
