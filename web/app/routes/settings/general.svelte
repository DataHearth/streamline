<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		Globe,
		Folder,
		Database,
		Lock,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config, markConfigForm } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import type { DiskUsage, SystemConfig, SystemInfo } from "@lib/types";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { INPUT_CLASS } from "@lib/form";

	// Saves per control rather than through one form, so nothing else
	// establishes the config-form context the field primitives read.
	markConfigForm();

	const qc = useQueryClient();

	const info = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
	}));

	const sys = createQuery<SystemConfig>(() => ({
		queryKey: ["config", "system"],
		queryFn: () => api<SystemConfig>("/config/system"),
	}));

	// Per-control saves, as on /settings/library: no form, no Save button, text
	// inputs commit on blur.
	const save = createMutation<
		SystemConfig,
		Error,
		Record<string, unknown>
	>(() => ({
		mutationFn: (body) =>
			api<SystemConfig>("/config/system", { method: "PATCH", body }),
		onSuccess: (resp) => {
			qc.setQueryData(["config", "system"], resp);
			toast.ok(i18n.system_settings_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	let drafts = $state<Record<string, string>>({});

	function draftOf(key: string, stored: string | number) {
		return drafts[key] ?? String(stored);
	}

	// Blank is meaningful for otel_endpoint (export off) but not for a
	// duration, so only the endpoint accepts an empty commit.
	function commitText(
		key: string,
		stored: string,
		save: (v: string) => void,
		allowEmpty = false,
	) {
		const next = (drafts[key] ?? "").trim();
		delete drafts[key];
		if (next === stored || (next === "" && !allowEmpty)) return;
		save(next);
	}

	// A secret's source, never its value.
	function secretLabel(source: SystemInfo["seed_admin_secret"]) {
		if (source === "file") return i18n.settings_secret_from_file();
		if (source === "config") return i18n.settings_secret_inline();
		return i18n.settings_secret_unset();
	}

	function barClass(kind: DiskUsage["kind"]) {
		if (kind === "err") return "bg-status-failed";
		if (kind === "warn") return "bg-status-wanted";
		return "bg-status-available";
	}
</script>

<header>
	<p class="max-w-2xl text-sm text-fg-muted">
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
	{#if info.data?.hardcover_auth_warn}
		<p class="mt-3 flex items-center gap-2">
			<a
				href="/settings/metadata#hardcover"
				onclick={() => sessionStorage.setItem("streamline:focus-field", "hardcover")}
				class="inline-flex items-center gap-1.5 rounded-full bg-status-wanted/14 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-wanted hover:bg-status-wanted/20"
			>
				{i18n.settings_hardcover_token_rejected()}
			</a>
		</p>
	{/if}
</header>

{#if info.isPending}
	<div class="mt-6 grid grid-cols-1 gap-3 md:grid-cols-2">
		<SkeletonList variant="field" count={6} />
	</div>
{:else if info.isError}
	<p class="mt-6 text-sm text-status-failed">
		{i18n.err_load_failed_detail({ reason: errorText(info.error) })}
	</p>
{:else if info.data}
	{@const d = info.data}
	<div class="mt-6 grid grid-cols-1 gap-3 md:grid-cols-2">
		{@render card(
			Globe,
			i18n.system_public_url(),
			d.public_url,
			true,
			d.https_warn ? { kind: "warn", label: i18n.settings_no_https() } : null,
		)}
		{@render storageCard(
			Folder,
			i18n.system_data_dir(),
			d.data_dir,
			d.data_usage,
			null,
		)}
		{@render storageCard(
			Database,
			i18n.system_database(),
			d.db_path,
			d.db_usage,
			d.db_size,
		)}
		{@render card(Lock, i18n.settings_auth_mode(), d.auth_mode, true, {
			kind: d.read_only ? "warn" : "ok",
			label: d.read_only
				? i18n.settings_readonly_config()
				: i18n.settings_login_required(),
		})}
	</div>

	<section class="mt-6 rounded-lg border border-border bg-bg-elevated">
		<header class="border-b border-border px-5 py-3.5">
			<h2 class="text-sm font-semibold text-fg">{i18n.settings_file_only()}</h2>
			<p class="mt-0.5 text-xs text-fg-muted">{i18n.settings_file_only_help()}</p>
		</header>
		<dl class="divide-y divide-border text-sm">
			{@render kv(
				i18n.settings_bind_address(),
				`${d.server_host ?? "?"}:${d.server_port ?? "?"}`,
			)}
			{@render kv(i18n.settings_auth_mode(), d.auth_mode)}
			{@render kv(
				i18n.settings_read_only_flag(),
				d.read_only ? i18n.common_yes() : i18n.common_no(),
			)}
			{@render kv(
				i18n.settings_trusted_proxies(),
				(d.trusted_proxies ?? []).join(", ") || i18n.settings_trusts_nobody(),
			)}
			{@render kv(
				i18n.settings_trusted_networks(),
				(d.trusted_networks ?? []).join(", ") || i18n.settings_trusts_nobody(),
			)}
			{#if (d.trusted_networks ?? []).length > 0}
				{@render kv(i18n.settings_trusted_role(), d.trusted_role ?? "")}
			{/if}
			{@render kv(
				i18n.settings_seed_admin(),
				d.seed_admin_email || i18n.settings_seed_admin_default(),
			)}
			{@render kv(
				i18n.settings_seed_admin_password(),
				secretLabel(d.seed_admin_secret),
			)}
			{@render kv(
				i18n.settings_session_secret(),
				d.session_secret_file || i18n.settings_secret_inline(),
			)}
			{#if d.torrent_listen_port}
				{@render kv(
					i18n.settings_torrent_port_override(),
					String(d.torrent_listen_port),
				)}
			{/if}
			{#if d.plex_client_id}
				{@render kv(i18n.settings_plex_client_id(), d.plex_client_id)}
			{/if}
			{#if d.tmdb_api_key_file}
				{@render kv("metadata.tmdb_api_key_file", d.tmdb_api_key_file)}
			{/if}
			{#if d.tvdb_api_key_file}
				{@render kv("metadata.tvdb_api_key_file", d.tvdb_api_key_file)}
			{/if}
			{@render kv(
				i18n.settings_otel_endpoint(),
				d.otel_endpoint || i18n.settings_otel_disabled(),
			)}
			{#if d.log_level}
				{@render kv(
					i18n.settings_log_sink(),
					`${d.log_level} / ${d.log_format ?? "text"}`,
				)}
			{/if}
		</dl>
	</section>

	<section class="mt-4 rounded-lg border border-border bg-bg-elevated">
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
			{@render kv(i18n.system_version(), d.version)}
			{@render kv(i18n.system_go_runtime(), d.go_version)}
			{@render kv(i18n.system_platform(), d.go_os_arch)}
			{#if d.commit}
				{@render kv(i18n.system_commit(), d.commit)}
			{/if}
			{#if d.built_at}
				{@render kv(i18n.system_built_at(), d.built_at)}
			{/if}
		</dl>
	</section>
{/if}

{#if sys.data}
	{@const s = sys.data}
	{#if s.restart_required}
		<div
			class="mt-6 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
		>
			<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<div>
				<p class="font-medium">{i18n.settings_restart_required()}</p>
				<p class="mt-0.5 text-status-wanted/80">
					{i18n.settings_changes_after_restart()}
				</p>
			</div>
		</div>
	{/if}

	<section class="mt-6 space-y-4 rounded-lg border border-border bg-bg-card p-4">
		<div>
			<h2 class="text-sm font-semibold text-fg">{i18n.system_telemetry()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.system_telemetry_help()}
			</p>
		</div>
		{@render textField(
			"otel",
			i18n.system_otel_endpoint(),
			i18n.system_otel_endpoint_help(),
			draftOf("otel", s.otel_endpoint),
			() =>
				commitText(
					"otel",
					s.otel_endpoint,
					(v) => save.mutate({ otel_endpoint: v }),
					true,
				),
			"localhost:4318",
		)}
	</section>

	<section class="mt-4 mb-6 space-y-4 rounded-lg border border-border bg-bg-card p-4">
		<div>
			<h2 class="text-sm font-semibold text-fg">{i18n.system_retention()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.system_retention_help()}
			</p>
		</div>
		{@render textField(
			"retention",
			i18n.system_events_retention(),
			i18n.system_events_retention_help(),
			draftOf("retention", s.events_retention),
			() =>
				commitText("retention", s.events_retention, (v) =>
					save.mutate({ events_retention: v }),
				),
			"2160h",
			"w-32",
		)}
	</section>
{/if}

{#snippet textField(
	key: string,
	label: string,
	help: string,
	value: string,
	commit: () => void,
	placeholder = "",
	width = "",
)}
	<label class="block">
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
			{label}
			<FieldLock locked={config.readOnly} />
		</span>
		<span class="block {width}">
			<input
				type="text"
				spellcheck="false"
				autocapitalize="off"
				autocomplete="off"
				readonly={config.readOnly}
				{value}
				{placeholder}
				oninput={(e) =>
					(drafts[key] = (e.currentTarget as HTMLInputElement).value)}
				onblur={commit}
				onkeydown={(e) => {
					if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
				}}
				class="{INPUT_CLASS} font-mono"
			/>
		</span>
		<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">{help}</p>
	</label>
{/snippet}

{#snippet card(
	Icon: typeof Globe,
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
					{i18n.disk_free_of({ free: usage.free, total: usage.total })}{#if meta} · {meta}{/if}
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
