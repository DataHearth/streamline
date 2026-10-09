<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { TriangleAlert } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config, markConfigForm } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import type {
		AppLogConfig,
		HTTPLogConfig,
		LogRotateConfig,
		SystemConfig,
	} from "@lib/types";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { INPUT_CLASS } from "@lib/form";

	// The application and HTTP access logs, out of General: they were most of
	// that page's editable surface, and the snapshot above them is what an
	// operator opens General for. Same endpoint, same per-control saves —
	// telemetry and event retention stay on General.
	markConfigForm();

	const qc = useQueryClient();

	const sys = createQuery<SystemConfig>(() => ({
		queryKey: ["config", "system"],
		queryFn: () => api<SystemConfig>("/config/system"),
	}));

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

	let appLog = $derived(sys.data?.log?.app ?? {});
	let httpLog = $derived(sys.data?.log?.http ?? {});
</script>

<header>
	<p class="max-w-2xl text-sm text-fg-muted">{i18n.settings_logs_intro()}</p>
</header>

{#if sys.isPending}
	<div class="mt-6 grid grid-cols-1 gap-3">
		<SkeletonList variant="field" count={4} />
	</div>
{:else if sys.isError}
	<p class="mt-6 text-sm text-status-failed">
		{i18n.err_load_failed_detail({ reason: errorText(sys.error) })}
	</p>
{:else if sys.data}
	{#if sys.data.restart_required}
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

	<section class="mt-4 mb-6 space-y-4 rounded-lg border border-border bg-bg-card p-4">
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
			<label class="block">
				<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.system_log_path()}
					<FieldLock locked={config.readOnly} />
				</span>
				<input
					type="text"
					spellcheck="false"
					autocapitalize="off"
					autocomplete="off"
					readonly={config.readOnly}
					value={pathValue(key, output)}
					placeholder="logs/streamline.log"
					oninput={(e) =>
						(drafts[key] = (e.currentTarget as HTMLInputElement).value)}
					onblur={() => commitPath(key, output, save)}
					onkeydown={(e) => {
						if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
					}}
					class="{INPUT_CLASS} font-mono"
				/>
				<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">
					{i18n.system_log_path_help()}
				</p>
			</label>
		{/if}
	</div>
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
			class="{INPUT_CLASS} font-mono"
		/>
	</label>
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
