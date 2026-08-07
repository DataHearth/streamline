<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Pause, Play, Zap, Pencil, Lock } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { createScheduleActions } from "../../lib/schedule-actions.svelte";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type { Schedule, ScheduleList } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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

	function statusBadge(s: Schedule) {
		if (s.running)
			return { cls: "bg-accent/15 text-accent", label: i18n.common_running_ellipsis() };
		if (s.paused)
			return { cls: "bg-surface text-fg-muted", label: i18n.status_paused() };
		switch (s.status) {
			case "success":
				return {
					cls: "bg-status-available/10 text-status-available",
					label: i18n.common_ok(),
				};
			case "error":
				return {
					cls: "bg-status-failed/10 text-status-failed",
					label: i18n.status_failed(),
				};
			case "skipped":
				return {
					cls: "bg-surface text-fg-muted",
					label: i18n.common_skipped(),
				};
			default:
				return {
					cls: "bg-surface text-fg-muted",
					label: i18n.common_never(),
				};
		}
	}

	const sb = $derived(statusBadge(row));
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
		<span
			class="inline-flex min-w-[4.75rem] items-center justify-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide {sb.cls}"
			title={row.last_error ?? undefined}
		>
			{sb.label}
		</span>
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
					system
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
					class={cn(
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
