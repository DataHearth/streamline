<script lang="ts">
	import { Pause, Play, Zap, Pencil, Lock, LoaderCircle } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { config, READONLY_HINT } from "@lib/config.svelte";
	import { createScheduleActions } from "@lib/schedule-actions.svelte";
	import { scheduleState } from "@lib/schedules-touch";
	import { formatRelative, formatDateTime } from "@lib/dates";
	import type { Schedule } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		row: Schedule;
		description: string | undefined;
		onEdit: (s: Schedule) => void;
	};

	let { row, description, onEdit }: Props = $props();

	// Shared with the touch action sheet so both patch the schedules cache the
	// same way; see lib/schedule-actions.
	const actions = createScheduleActions();
	const pause = actions.pause;
	const resume = actions.resume;
	const run = actions.run;

	const toggle = $derived(
		row.paused
			? {
					mutate: () => resume.mutate(row.name),
					pending: resume.isPending,
					label: i18n.schedule_resume(),
					cls: "border-status-available/40 text-status-available hover:border-status-available hover:bg-status-available/10",
				}
			: {
					mutate: () => pause.mutate(row.name),
					pending: pause.isPending,
					label: i18n.schedule_pause(),
					cls: "border-status-wanted/40 text-status-wanted hover:border-status-wanted hover:bg-status-wanted/10",
				},
	);

	const st = $derived(scheduleState(row));
	// A state that needs no attention gets a hollow dot, so the filled ones
	// (healthy, failed) are what the eye lands on down the column.
	const quiet = $derived(
		st.key === "paused" || st.key === "skipped" || st.key === "never",
	);
	// Only a known total draws a bar; a bare count has nothing to fill against.
	const percent = $derived(
		row.running && row.progress && row.progress.total > 0
			? Math.min(100, (100 * row.progress.done) / row.progress.total)
			: null,
	);
	const startedTitle = $derived(
		row.running && row.last_started_at
			? i18n.schedule_started({ when: formatRelative(row.last_started_at) })
			: undefined,
	);
</script>

<tr class={row.paused ? "bg-surface/40" : ""}>
	<td class="px-4 py-3 align-middle">
		<div class="font-mono text-[12.5px] text-fg">{row.name}</div>
		{#if description}
			<div class="mt-0.5 text-[11.5px] text-fg-muted">{description}</div>
		{/if}
	</td>
	<td class="px-4 py-3 align-middle">
		<span
			class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-xs text-fg"
			>{row.interval}</span
		>
	</td>
	<td class="px-4 py-3 align-middle text-fg">
		{#if row.last_finished_at}
			<span title={formatDateTime(row.last_finished_at)}>
				{formatRelative(row.last_finished_at)}
			</span>
		{:else}
			<span class="text-fg-muted">—</span>
		{/if}
	</td>
	<td class="px-4 py-3 align-middle">
		<div
			class={twMerge("flex items-center gap-2 text-[13px] font-medium", st.tone)}
			title={startedTitle}
		>
			{#if st.key === "running"}
				<LoaderCircle size={13} class="shrink-0 animate-spin" aria-hidden="true" />
			{:else}
				<span
					aria-hidden="true"
					class={twMerge(
						"size-1.5 shrink-0 rounded-full",
						quiet ? "border border-current" : "bg-current",
					)}
				></span>
			{/if}
			<span class="whitespace-nowrap">{st.label}</span>
		</div>
		{#if percent !== null}
			<div
				class="mt-1.5 h-0.5 max-w-[7rem] overflow-hidden rounded-full bg-accent/20"
				role="progressbar"
				aria-valuemin="0"
				aria-valuemax="100"
				aria-valuenow={Math.round(percent)}
			>
				<div class="progress-fill h-full bg-accent" style:--progress="{percent}%"></div>
			</div>
		{:else if st.key === "failed" && row.last_error}
			<div
				class="mt-0.5 max-w-[13rem] truncate pl-3.5 text-[11.5px] text-fg-subtle"
				title={row.last_error}
			>
				{row.last_error}
			</div>
		{/if}
	</td>
	<td class="px-4 py-3 align-middle text-fg">
		{#if row.paused || row.running}
			<span class="text-fg-muted">—</span>
		{:else if row.next_run_at}
			<span title={formatDateTime(row.next_run_at)}>
				{formatRelative(row.next_run_at)}
			</span>
		{:else}
			<span class="text-fg-muted">—</span>
		{/if}
	</td>
	<td class="px-4 py-3 align-middle">
		<div
			class="flex {row.system ? 'justify-center' : 'justify-end'} gap-1"
		>
			{#if row.system}
				<span
					class="inline-flex items-center gap-1.5 rounded-full border border-dashed border-border bg-bg-card px-2.5 py-1 text-xs font-medium text-fg-muted"
					title={i18n.settings_managed_by_streamline()}
				>
					<Lock size={12} aria-hidden="true" />
					{i18n.schedule_system()}
				</span>
			{:else}
				<button
					type="button"
					disabled={row.running || run.isPending}
					onclick={() => run.mutate(row.name)}
					class="inline-flex h-7 w-7 items-center justify-center rounded border border-accent/40 text-accent transition hover:border-accent hover:bg-accent/10 disabled:cursor-not-allowed disabled:opacity-40"
					title={i18n.schedule_run_now()}
				>
					<Zap size={14} aria-hidden="true" />
				</button>
				<button
					type="button"
					disabled={toggle.pending}
					onclick={toggle.mutate}
					class={twMerge(
						"inline-flex h-7 w-7 items-center justify-center rounded border transition disabled:cursor-not-allowed disabled:opacity-40",
						toggle.cls,
					)}
					title={toggle.label}
					aria-label={toggle.label}
				>
					{#if row.paused}
						<Play size={14} aria-hidden="true" />
					{:else}
						<Pause size={14} aria-hidden="true" />
					{/if}
				</button>
				<button
					type="button"
					disabled={config.readOnly}
					onclick={() => onEdit(row)}
					class="inline-flex h-7 w-7 items-center justify-center rounded border border-accent/40 text-accent transition hover:border-accent hover:bg-accent/10 disabled:cursor-not-allowed disabled:opacity-40"
					title={config.readOnly ? READONLY_HINT : i18n.schedule_edit_interval()}
				>
					<Pencil size={14} aria-hidden="true" />
				</button>
			{/if}
		</div>
	</td>
</tr>

<style>
	.progress-fill {
		width: var(--progress);
		transition: width 300ms ease-out;
	}
</style>
