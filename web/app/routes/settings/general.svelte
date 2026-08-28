<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		Play,
		Globe,
		Folder,
		Database,
		Lock,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type {
		AppLogConfig,
		DiskUsage,
		HTTPLogConfig,
		LogRotateConfig,
		SystemConfig,
		SystemInfo,
	} from "../../lib/types";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import Select from "../../components/forms/Select.svelte";
	import FieldLock from "../../components/forms/FieldLock.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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

	function saveApp(patch: AppLogConfig) {
		save.mutate({ log: { app: patch } });
	}
	function saveHTTP(patch: HTTPLogConfig) {
		save.mutate({ log: { http: patch } });
	}

	let drafts = $state<Record<string, string>>({});

	function draftOf(key: string, stored: string | number) {
		return drafts[key] ?? String(stored);
	}

	// Blank is meaningful for otel_endpoint (export off) but not for a log
	// output or a duration, so only the endpoint accepts an empty commit.
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

	function commitNumber(key: string, stored: number, save: (v: number) => void) {
		const raw = drafts[key];
		delete drafts[key];
		if (raw === undefined) return;
		const n = Math.round(Number(raw));
		if (!Number.isFinite(n) || n < 0 || n === stored) return;
		save(n);
	}

	// Rotation only does anything when the log goes to a file — lumberjack is
	// never in the path for a stream, so showing the four knobs there would be
	// four controls that provably change nothing.
	function isFile(output: string | undefined) {
		return Boolean(output) && output !== "stderr" && output !== "stdout";
	}

	// An output is one of two streams or a file path — three choices, so a
	// picker rather than a text field an operator has to know the vocabulary
	// for. `pendingFile` is what lets "file" be *selected* before a path has
	// been typed: an empty path is not an output, so nothing saves until one is.
	let pendingFile = $state<Record<string, boolean>>({});

	function outputMode(key: string, output: string | undefined) {
		if (pendingFile[key]) return "file";
		if (output === "stdout") return "stdout";
		return isFile(output) ? "file" : "stderr";
	}

	function setOutputMode(
		key: string,
		mode: string,
		save: (v: string) => void,
	) {
		if (mode === "file") {
			pendingFile[key] = true;
			return;
		}
		delete pendingFile[key];
		delete drafts[key];
		save(mode);
	}

	function pathValue(key: string, output: string | undefined) {
		return drafts[key] ?? (isFile(output) ? (output ?? "") : "");
	}

	function commitPath(
		key: string,
		output: string | undefined,
		save: (v: string) => void,
	) {
		const next = (drafts[key] ?? "").trim();
		delete drafts[key];
		// Keep the field on screen while it is still empty — the picker says
		// "file" and there is nothing yet to save.
		if (next === "" || next === output) return;
		delete pendingFile[key];
		save(next);
	}

	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

	let appLog = $derived(sys.data?.log?.app ?? {});
	let httpLog = $derived(sys.data?.log?.http ?? {});

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
			<h2 class="text-sm font-semibold text-fg">{i18n.system_app_log()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.system_app_log_help()}
			</p>
		</div>

		<Checkbox
			checked={appLog.enabled ?? true}
			disabled={save.isPending}
			onChange={(v) => saveApp({ enabled: v })}
			label={i18n.system_log_enabled()}
			description={i18n.system_app_log_enabled_help()}
		/>

		<div class="grid gap-4 sm:grid-cols-2">
			<Select
				label={i18n.system_log_level()}
				value={appLog.level ?? "info"}
				options={[
					{ value: "debug", label: "debug" },
					{ value: "info", label: "info" },
					{ value: "warn", label: "warn" },
					{ value: "error", label: "error" },
				]}
				onChange={(v) => saveApp({ level: v as AppLogConfig["level"] })}
			/>
			<Select
				label={i18n.system_log_format()}
				value={appLog.format ?? "text"}
				options={[
					{ value: "text", label: "text", hint: i18n.system_format_text_hint() },
					{ value: "json", label: "json", hint: i18n.system_format_json_hint() },
				]}
				onChange={(v) => saveApp({ format: v as AppLogConfig["format"] })}
			/>
		</div>

		{@render outputPicker("app_output", appLog.output, (v) =>
			saveApp({ output: v }),
		)}

		{#if isFile(appLog.output)}
			{@render rotateFields("app", appLog.rotate ?? {}, (r) =>
				saveApp({ rotate: r }),
			)}
		{/if}
	</section>

	<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
		<div>
			<h2 class="text-sm font-semibold text-fg">{i18n.system_http_log()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.system_http_log_help()}
			</p>
		</div>

		<Checkbox
			checked={httpLog.enabled ?? true}
			disabled={save.isPending}
			onChange={(v) => saveHTTP({ enabled: v })}
			label={i18n.system_log_enabled()}
			description={i18n.system_http_log_enabled_help()}
		/>

		<div class="max-w-xs">
			<Select
				label={i18n.system_log_format()}
				value={httpLog.format ?? "json"}
				options={[
					{ value: "json", label: "json", hint: i18n.system_format_json_hint() },
					{
						value: "combined",
						label: "combined",
						hint: i18n.system_format_combined_hint(),
					},
				]}
				onChange={(v) => saveHTTP({ format: v as HTTPLogConfig["format"] })}
			/>
		</div>

		{@render outputPicker("http_output", httpLog.output, (v) =>
			saveHTTP({ output: v }),
		)}

		{#if isFile(httpLog.output)}
			{@render rotateFields("http", httpLog.rotate ?? {}, (r) =>
				saveHTTP({ rotate: r }),
			)}
		{/if}
	</section>

	<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
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

{#snippet outputPicker(
	key: string,
	output: string | undefined,
	save: (v: string) => void,
)}
	<div class="space-y-3">
		<div class="max-w-xs">
			<Select
				label={i18n.system_log_output()}
				value={outputMode(key, output)}
				options={[
					{
						value: "stderr",
						label: "stderr",
						hint: i18n.system_output_stderr_hint(),
					},
					{
						value: "stdout",
						label: "stdout",
						hint: i18n.system_output_stdout_hint(),
					},
					{
						value: "file",
						label: i18n.system_output_file(),
						hint: i18n.system_output_file_hint(),
					},
				]}
				onChange={(v) => setOutputMode(key, v, save)}
			/>
		</div>
		{#if outputMode(key, output) === "file"}
			{@render textField(
				key,
				i18n.system_log_path(),
				i18n.system_log_path_help(),
				pathValue(key, output),
				() => commitPath(key, output, save),
				"logs/streamline.log",
			)}
		{/if}
	</div>
{/snippet}

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
				class="{inputClass} font-mono"
			/>
		</span>
		<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">{help}</p>
	</label>
{/snippet}

{#snippet rotateFields(
	prefix: string,
	r: LogRotateConfig,
	commit: (r: LogRotateConfig) => void,
)}
	<div class="rounded-md border border-border bg-bg-deep/40 p-3">
		<p class="text-xs font-medium text-fg-muted">{i18n.system_rotation()}</p>
		<p class="mt-0.5 text-xs text-fg-subtle">{i18n.system_rotation_help()}</p>
		<div class="mt-3 grid gap-3 sm:grid-cols-3">
			{@render numberField(
				`${prefix}_size`,
				i18n.system_rotate_size(),
				draftOf(`${prefix}_size`, r.max_size_mb ?? 0),
				() =>
					commitNumber(`${prefix}_size`, r.max_size_mb ?? 0, (v) =>
						commit({ max_size_mb: v }),
					),
			)}
			{@render numberField(
				`${prefix}_backups`,
				i18n.system_rotate_backups(),
				draftOf(`${prefix}_backups`, r.max_backups ?? 0),
				() =>
					commitNumber(`${prefix}_backups`, r.max_backups ?? 0, (v) =>
						commit({ max_backups: v }),
					),
			)}
			{@render numberField(
				`${prefix}_age`,
				i18n.system_rotate_age(),
				draftOf(`${prefix}_age`, r.max_age_days ?? 0),
				() =>
					commitNumber(`${prefix}_age`, r.max_age_days ?? 0, (v) =>
						commit({ max_age_days: v }),
					),
			)}
		</div>
		<div class="mt-3">
			<Checkbox
				checked={r.compress ?? false}
				disabled={save.isPending}
				onChange={(v) => commit({ compress: v })}
				label={i18n.system_rotate_compress()}
			/>
		</div>
	</div>
{/snippet}

{#snippet numberField(
	key: string,
	label: string,
	value: string,
	commit: () => void,
)}
	<label class="block">
		<span class="mb-1 flex items-center gap-1.5 text-xs font-medium text-fg">
			{label}
			<FieldLock locked={config.readOnly} />
		</span>
		<input
			type="number"
			min="0"
			inputmode="numeric"
			readonly={config.readOnly}
			{value}
			oninput={(e) => (drafts[key] = (e.currentTarget as HTMLInputElement).value)}
			onblur={commit}
			onkeydown={(e) => {
				if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
			}}
			class="{inputClass} font-mono"
		/>
	</label>
{/snippet}

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

<style>
	/* Match TextField: drop the native spin buttons. */
	input[type="number"]::-webkit-inner-spin-button,
	input[type="number"]::-webkit-outer-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	input[type="number"] {
		-moz-appearance: textfield;
		appearance: textfield;
	}
</style>
