<script lang="ts">
	import { slide } from "svelte/transition";
	import { ArrowUpRight, Ban, ChevronDown, LoaderCircle, RotateCcw } from "@lucide/svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import { formatBytes } from "../../lib/format";
	import { formatDateTime } from "../../lib/dates";
	import { basename, jobFigure, jobHref, transcodeKind } from "../../lib/transcoding";
	import type { TranscodeJob } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// One row for every width. There is no column grid, so each status brings
	// only the lines it has: running grows a bar, failed grows an error, canceled
	// is two lines and nothing else. Below sm the figure block drops under the
	// title rather than becoming a different component.
	let {
		job,
		expanded = false,
		canControl = false,
		busy = false,
		onToggle,
		onCancel,
		onRetry,
	}: {
		job: TranscodeJob;
		expanded?: boolean;
		canControl?: boolean;
		busy?: boolean;
		onToggle: (id: number) => void;
		onCancel: (job: TranscodeJob) => void;
		onRetry: (job: TranscodeJob) => void;
	} = $props();

	let kind = $derived(transcodeKind(job.status));
	let fig = $derived(jobFigure(job));
	let running = $derived(job.status === "running");
	let cancellable = $derived(running || job.status === "queued");
	let retryable = $derived(job.status === "failed");
	// The stderr tail is up to ten lines; the row shows the first, which is
	// almost always the real cause, and the block holds the rest.
	let errorHead = $derived(job.error?.split("\n")[0] ?? "");
	let href = $derived(jobHref(job));

	const action =
		"inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-md px-3 text-[13px] font-semibold transition disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring lg:h-8 lg:min-h-0 lg:text-xs";
</script>

<div
	class={cn(
		"border-b border-border last:border-b-0",
		job.status === "canceled" && "opacity-60",
	)}
>
	<div
		class="flex flex-col gap-2.5 px-3 py-3 sm:flex-row sm:items-start sm:gap-4 md:px-4"
	>
		<button
			type="button"
			onclick={() => onToggle(job.id)}
			aria-expanded={expanded}
			class="min-w-0 flex-1 rounded-sm text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			<span class="flex min-w-0 items-center gap-2.5">
				<StatusPill status={kind} size="sm" live={running} />
				<span class="min-w-0 flex-1 truncate text-[13.5px] font-semibold tracking-[-0.01em] text-fg">
					{job.media_title}
				</span>
				<ChevronDown
					size={13}
					class={cn(
						"shrink-0 text-fg-faint transition-transform",
						expanded && "rotate-180",
					)}
					aria-hidden="true"
				/>
			</span>
			<span class="mt-0.5 block truncate font-mono text-[11px] text-fg-subtle">
				{basename(job.file_path)}
			</span>
			{#if running}
				<span class="mt-2 block">
					<ProgressBar
						value={job.percent === undefined ? undefined : job.percent / 100}
						status={kind}
						height={4}
						shimmer={job.percent !== undefined}
						label={i18n.common_progress()}
					/>
				</span>
			{/if}
			{#if errorHead && !expanded}
				<span
					class="mt-1.5 block truncate font-mono text-[11px] text-status-failed"
				>
					{errorHead}
				</span>
			{/if}
		</button>

		<div class="flex shrink-0 items-center justify-between gap-3 sm:justify-end">
			<div class="min-w-0 sm:max-w-[230px] sm:text-right">
				{#if fig.value}
					<div
						class="truncate font-mono text-xs tabular-nums"
						style:color={fig.tone ?? "var(--fg-muted)"}
					>
						{fig.value}
						{#if fig.saved}
							<span class="saved ml-1 rounded-sm px-1.5 py-px text-[10.5px] font-medium">
								{fig.saved}
							</span>
						{/if}
					</div>
				{/if}
				<div class="truncate font-mono text-[10.5px] tabular-nums text-fg-faint">
					{fig.sub}
				</div>
			</div>
			{#if canControl && cancellable}
				<button
					type="button"
					disabled={busy}
					onclick={() => onCancel(job)}
					class={cn(action, "bg-status-failed/15 text-status-failed hover:bg-status-failed/25")}
				>
					{#if busy}
						<LoaderCircle size={13} class="motion-safe:animate-spin" aria-hidden="true" />
					{:else}
						<Ban size={13} aria-hidden="true" />
					{/if}
					{i18n.common_cancel()}
				</button>
			{:else if canControl && retryable}
				<button
					type="button"
					disabled={busy}
					onclick={() => onRetry(job)}
					class={cn(action, "bg-accent-soft text-accent-text hover:bg-accent-soft")}
				>
					{#if busy}
						<LoaderCircle size={13} class="motion-safe:animate-spin" aria-hidden="true" />
					{:else}
						<RotateCcw size={13} aria-hidden="true" />
					{/if}
					{i18n.transcode_retry()}
				</button>
			{/if}
		</div>
	</div>

	{#if expanded}
		<div transition:slide={{ duration: 180 }} class="bg-bg-card px-3 py-3.5 md:px-4">
			<dl class="grid grid-cols-[max-content_1fr] gap-x-5 gap-y-1.5 text-[11.5px]">
				<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">
					{i18n.transcode_file()}
				</dt>
				<dd class="m-0 min-w-0 break-all font-mono text-fg-muted">{job.file_path}</dd>
				<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">
					{i18n.common_size()}
				</dt>
				<dd class="m-0 min-w-0 font-mono text-fg-muted">
					{#if job.size_after}
						{formatBytes(job.size_before)} → {formatBytes(job.size_after)}
					{:else}
						{formatBytes(job.size_before)}
					{/if}
				</dd>
				<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">
					{i18n.transcode_attempts_label()}
				</dt>
				<dd class="m-0 min-w-0 font-mono text-fg-muted">{job.attempts}</dd>
				<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">
					{i18n.common_created()}
				</dt>
				<dd class="m-0 min-w-0 font-mono text-fg-muted">
					{formatDateTime(job.created_at)}
				</dd>
				{#if job.finished_at}
					<dt class="font-medium uppercase tracking-[0.1em] text-fg-faint">
						{i18n.transcode_finished_label()}
					</dt>
					<dd class="m-0 min-w-0 font-mono text-fg-muted">
						{formatDateTime(job.finished_at)}
					</dd>
				{/if}
			</dl>
			{#if href}
				<a
					{href}
					class="mt-3 inline-flex min-h-11 items-center gap-1.5 rounded-md border border-border px-3 text-[12.5px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg lg:h-8 lg:min-h-0"
				>
					{i18n.transcode_open_item({ title: job.media_title })}
					<ArrowUpRight size={13} aria-hidden="true" />
				</a>
			{/if}
			{#if job.error}
				<pre
					class="errbox mt-3 overflow-x-auto rounded-md p-3 font-mono text-[11px] leading-relaxed text-status-failed">{job.error}</pre>
			{/if}
		</div>
	{/if}
</div>

<style>
	/* The saved badge and the stderr block are both tinted from a status token,
	   which the utility layer may not have generated a class for this late. */
	.saved {
		background-color: color-mix(in srgb, var(--status-succeeded) 15%, transparent);
		color: var(--status-succeeded);
	}
	.errbox {
		white-space: pre-wrap;
		background-color: var(--bg-deep);
		border: 1px solid color-mix(in srgb, var(--status-failed) 25%, transparent);
	}
</style>
