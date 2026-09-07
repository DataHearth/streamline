<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { ArrowUpRight, LoaderCircle, Radar, TriangleAlert } from "@lucide/svelte";
	import { api, errorText, ApiError } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type { FFmpegConfig, TranscodeConfig } from "../../lib/types";
	import { scanWorkerUnavailable } from "../../lib/transcoding";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import FieldLock from "../../components/forms/FieldLock.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const transcoding = createQuery<TranscodeConfig>(() => ({
		queryKey: ["config", "transcoding"],
		queryFn: () => api<TranscodeConfig>("/config/transcoding"),
	}));
	// The binary belongs to Media probe — one surface owns it, and the two
	// duplicate ffmpeg_path keys in the original transcoding config block should
	// not exist. This page only reads it back.
	const ffmpeg = createQuery<FFmpegConfig>(() => ({
		queryKey: ["config", "ffmpeg"],
		queryFn: () => api<FFmpegConfig>("/config/ffmpeg"),
	}));

	const save = createMutation<TranscodeConfig, Error, Partial<TranscodeConfig>>(
		() => ({
			mutationFn: (body) =>
				api<TranscodeConfig>("/config/transcoding", { method: "PATCH", body }),
			onSuccess: (resp) => {
				qc.setQueryData(["config", "transcoding"], resp);
				qc.invalidateQueries({ queryKey: ["system", "info"] });
				toast.ok(i18n.transcode_saved());
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	// No progress signal exists for a scan — it queues jobs and returns. So the
	// button reports that it started rather than tracking something it cannot see.
	let scanStarted = $state(false);
	const scan = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>("/transcoding/scan", { method: "POST" }),
		onSuccess: () => {
			scanStarted = true;
			toast.ok(i18n.transcode_scan_started());
		},
		onError: (e) => {
			if (scanWorkerUnavailable(e)) {
				toast.err(i18n.transcode_scan_no_ffmpeg());
				return;
			}
			if (e instanceof ApiError && e.status === 409) {
				scanStarted = true;
				toast.err(i18n.transcode_scan_running());
				return;
			}
			toast.err(errorText(e));
		},
	}));

	// Every control saves on its own, as on Media probe — so there is no form and
	// no Save button, and the two number fields commit on blur rather than on
	// each keystroke.
	let concurrentDraft = $state<string | null>(null);
	let failuresDraft = $state<string | null>(null);
	let concurrent = $derived(
		concurrentDraft ?? String(transcoding.data?.max_concurrent ?? 1),
	);
	let failures = $derived(
		failuresDraft ?? String(transcoding.data?.max_failures ?? 3),
	);

	function commitNumber(
		raw: string | null,
		current: number | undefined,
		min: number,
		max: number,
		apply: (v: number) => void,
	) {
		if (raw === null) return;
		const n = Number(raw);
		if (!Number.isFinite(n)) return;
		const clamped = Math.min(max, Math.max(min, Math.round(n)));
		if (clamped === current) return;
		apply(clamped);
	}

	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

	let pending = $derived(transcoding.isPending || ffmpeg.isPending);
	let failed = $derived(transcoding.isError || ffmpeg.isError);
	// The worker refuses to run with ffmpeg switched off just as with the
	// binary missing, but `missing` cannot say so: it is gated on `enabled`.
	let ffmpegOff = $derived(ffmpeg.data?.enabled === false);
	let missing = $derived(
		Boolean(ffmpeg.data?.enabled) && ffmpeg.data?.found === false,
	);
	let resolved = $derived(
		[ffmpeg.data?.resolved_path, ffmpeg.data?.version].filter(Boolean).join(" · "),
	);
	let locked = $derived(config.readOnly || save.isPending);
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.settings_transcoding()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">{i18n.transcode_settings_intro()}</p>
	</header>

	{#if pending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if failed}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({
				reason: errorText(transcoding.error ?? ffmpeg.error),
			})}
		</p>
	{:else if transcoding.data}
		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.transcode_engine()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.transcode_engine_help()}
			</p>

			<div class="mt-4">
				<Checkbox
					checked={transcoding.data.enabled}
					disabled={locked}
					onChange={(v) => save.mutate({ enabled: v })}
					label={i18n.transcode_enable()}
					description={i18n.transcode_enable_help()}
				/>
			</div>

			{#if missing}
				<div
					class="mt-4 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs leading-relaxed text-status-wanted"
				>
					<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
					<span>{i18n.probe_not_found()}</span>
				</div>
			{/if}

			<div class="mt-4 grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
						{i18n.transcode_max_concurrent()}
						<FieldLock locked={config.readOnly} />
					</span>
					<input
						type="number"
						min="1"
						max="8"
						inputmode="numeric"
						readonly={config.readOnly}
						value={concurrent}
						oninput={(e) => (concurrentDraft = (e.currentTarget as HTMLInputElement).value)}
						onblur={() => {
							const raw = concurrentDraft;
							concurrentDraft = null;
							commitNumber(raw, transcoding.data?.max_concurrent, 1, 8, (v) =>
								save.mutate({ max_concurrent: v }),
							);
						}}
						onkeydown={(e) => {
							if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
						}}
						class="{inputClass} font-mono tabular-nums"
					/>
					<p class="mt-1 text-xs text-fg-muted">{i18n.transcode_max_concurrent_help()}</p>
				</label>

				<label class="block">
					<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
						{i18n.transcode_max_failures()}
						<FieldLock locked={config.readOnly} />
					</span>
					<input
						type="number"
						min="1"
						max="10"
						inputmode="numeric"
						readonly={config.readOnly}
						value={failures}
						oninput={(e) => (failuresDraft = (e.currentTarget as HTMLInputElement).value)}
						onblur={() => {
							const raw = failuresDraft;
							failuresDraft = null;
							commitNumber(raw, transcoding.data?.max_failures, 1, 10, (v) =>
								save.mutate({ max_failures: v }),
							);
						}}
						onkeydown={(e) => {
							if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
						}}
						class="{inputClass} font-mono tabular-nums"
					/>
					<p class="mt-1 text-xs text-fg-muted">{i18n.transcode_max_failures_help()}</p>
				</label>
			</div>

			<div
				class="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-bg px-3 py-2.5"
			>
				<span class="text-xs text-fg-subtle">{i18n.probe_ffmpeg()}</span>
				<span class="min-w-0 truncate font-mono text-xs text-fg-muted">
					{missing ? i18n.transcode_ffmpeg_unresolved() : resolved || "—"}
				</span>
			</div>
			<p class="mt-1.5 text-xs text-fg-muted">
				{i18n.transcode_ffmpeg_help()}
				<a
					href="/settings/media-probe"
					class="inline-flex items-center gap-0.5 text-accent-text hover:text-accent-hover"
				>
					{i18n.settings_media_probe()}
					<ArrowUpRight size={12} aria-hidden="true" />
				</a>
			</p>
		</section>

		<section class="mt-4 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.transcode_scan()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.transcode_scan_help()}
			</p>

			<div class="mt-4 flex flex-wrap items-center gap-3">
				<button
					type="button"
					disabled={config.readOnly || scan.isPending || scanStarted || missing || ffmpegOff || !transcoding.data.enabled}
					onclick={() => scan.mutate()}
					class="inline-flex min-h-11 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0"
				>
					{#if scan.isPending}
						<LoaderCircle size={14} class="motion-safe:animate-spin" aria-hidden="true" />
					{:else}
						<Radar size={14} aria-hidden="true" />
					{/if}
					{i18n.transcode_scan()}
				</button>
				{#if scanStarted}
					<a
						href="/activity/transcoding"
						class="inline-flex items-center gap-1 text-xs text-accent-text hover:text-accent-hover"
					>
						{i18n.transcode_scan_started()}
						<ArrowUpRight size={12} aria-hidden="true" />
					</a>
				{:else if !transcoding.data.enabled}
					<span class="text-xs text-fg-muted">{i18n.transcode_scan_needs_enable()}</span>
				{:else if ffmpegOff}
					<span class="text-xs text-fg-muted">{i18n.transcode_scan_needs_ffmpeg()}</span>
				{/if}
			</div>
		</section>
	{/if}
</div>

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
