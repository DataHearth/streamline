<script lang="ts">
	import { CircleX, Replace, TriangleAlert } from "@lucide/svelte";
	import TranscodeRow from "./TranscodeRow.svelte";
	import { errorText } from "../../lib/api";
	import type { TranscodeJob } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The list and everything it can be instead of a list. The three empty
	// flavours are separate states, not one message with a variable in it: an
	// operator who turned transcoding off, one whose ffmpeg went missing and one
	// whose library is simply compliant need three different next steps.
	let {
		rows,
		loading,
		error = null,
		disabled = false,
		ffmpegMissing = false,
		canControl = false,
		expandedId = null,
		busyId = null,
		onToggle,
		onCancel,
		onRetry,
		onScan,
	}: {
		rows: TranscodeJob[];
		loading: boolean;
		error?: Error | null;
		disabled?: boolean;
		ffmpegMissing?: boolean;
		canControl?: boolean;
		expandedId?: number | null;
		busyId?: number | null;
		onToggle: (id: number) => void;
		onCancel: (job: TranscodeJob) => void;
		onRetry: (job: TranscodeJob) => void;
		onScan?: () => void;
	} = $props();

	const shell =
		"mt-3 overflow-hidden rounded-xl border border-border bg-bg-elevated";
	const empty =
		"mt-3 flex flex-col items-center gap-2.5 rounded-xl border border-dashed border-border bg-bg-elevated px-5 py-12 text-center md:py-16";
	const link =
		"inline-flex min-h-11 items-center gap-1.5 rounded-md border border-border px-3.5 text-[13px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg lg:h-9 lg:min-h-0";
</script>

{#if disabled}
	<div class={empty}>
		<div class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle">
			<CircleX size={20} aria-hidden="true" />
		</div>
		<p class="text-sm font-semibold text-fg">{i18n.transcode_off_title()}</p>
		<p class="max-w-[34ch] text-xs leading-relaxed text-fg-muted">
			{i18n.transcode_off_help()}
		</p>
		{#if canControl}
			<a href="/settings/transcoding" class={link}>{i18n.transcode_open_settings()}</a>
		{/if}
	</div>
{:else if ffmpegMissing}
	<div class={empty}>
		<div class="grid h-11 w-11 place-items-center rounded-full bg-status-wanted/15 text-status-wanted">
			<TriangleAlert size={20} aria-hidden="true" />
		</div>
		<p class="text-sm font-semibold text-fg">{i18n.transcode_no_ffmpeg_title()}</p>
		<p class="max-w-[34ch] text-xs leading-relaxed text-fg-muted">
			{i18n.probe_not_found()}
		</p>
		{#if canControl}
			<a href="/settings/media-probe" class={link}>{i18n.settings_media_probe()}</a>
		{/if}
	</div>
{:else if loading}
	<div class={shell}>
		{#each Array(4) as _, i (i)}
			<div class="flex items-center gap-3 border-b border-border px-3 py-4 last:border-b-0 md:px-4">
				<div class="flex-1 space-y-2">
					<div class="h-3 w-2/5 rounded bg-surface motion-safe:animate-pulse"></div>
					<div class="h-2.5 w-3/5 rounded bg-surface motion-safe:animate-pulse"></div>
				</div>
				<div class="h-3 w-20 shrink-0 rounded bg-surface motion-safe:animate-pulse"></div>
			</div>
		{/each}
	</div>
{:else if error}
	<div class="mt-3 rounded-xl border border-status-failed/25 bg-bg-elevated px-5 py-10 text-center">
		<p class="text-sm font-semibold text-status-failed">
			{i18n.transcode_load_failed()}
		</p>
		<p class="mt-1 font-mono text-[11px] text-fg-subtle">{errorText(error)}</p>
	</div>
{:else if rows.length === 0}
	<div class={empty}>
		<div class="grid h-11 w-11 place-items-center rounded-full bg-accent-soft text-accent-text">
			<Replace size={20} aria-hidden="true" />
		</div>
		<p class="text-sm font-semibold text-fg">{i18n.transcode_empty_title()}</p>
		<p class="max-w-[36ch] text-xs leading-relaxed text-fg-muted">
			{i18n.transcode_empty_help()}
		</p>
		{#if canControl}
			<div class="mt-0.5 flex flex-wrap items-center justify-center gap-2">
				<a href="/settings/quality-profiles" class={link}>
					{i18n.settings_quality_profiles()}
				</a>
				{#if onScan}
					<button
						type="button"
						onclick={() => onScan?.()}
						class="inline-flex min-h-11 items-center gap-1.5 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover lg:h-9 lg:min-h-0"
					>
						{i18n.transcode_scan()}
					</button>
				{/if}
			</div>
		{/if}
	</div>
{:else}
	<div class={shell}>
		{#each rows as job (job.id)}
			<TranscodeRow
				{job}
				{canControl}
				expanded={expandedId === job.id}
				busy={busyId === job.id}
				{onToggle}
				{onCancel}
				{onRetry}
			/>
		{/each}
	</div>
{/if}
