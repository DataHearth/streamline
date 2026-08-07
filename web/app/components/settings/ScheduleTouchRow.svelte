<script lang="ts">
	import { MoreHorizontal, Lock } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import { scheduleState } from "../../lib/schedules-touch";
	import type { Schedule } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The touch row. Six table columns become two lines and one trailing word:
	// the job's name and what it does, then interval and last run underneath,
	// then the state — which for a healthy job is the thing you actually came to
	// read ("Runs in 11m") rather than the word "OK".
	let {
		row,
		description,
		onMenu,
	}: {
		row: Schedule;
		description: string | undefined;
		onMenu: (s: Schedule) => void;
	} = $props();

	let state = $derived(scheduleState(row));

	// A healthy job says when it next runs; anything else says what it is.
	let trailing = $derived(
		state.key === "ok" && row.next_run_at
			? i18n.schedule_runs_in({ when: formatRelative(row.next_run_at) })
			: state.label,
	);
</script>

<div class="flex min-h-[64px] items-center gap-3 px-4 py-3">
	<div class="min-w-0 flex-1">
		<div class="truncate font-mono text-[13px] text-fg">{row.name}</div>
		{#if description}
			<div class="mt-0.5 truncate text-xs text-fg-subtle">{description}</div>
		{/if}
		<div class="mt-1.5 flex items-center gap-2">
			<span
				class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-[11px] text-fg-muted"
			>
				{row.interval}
			</span>
			<span class="truncate text-[11.5px] text-fg-subtle">
				{#if row.last_finished_at}
					<span title={formatDateTime(row.last_finished_at)}>
						{i18n.schedule_last_ran({
							when: formatRelative(row.last_finished_at),
						})}
					</span>
				{:else}
					{i18n.schedule_never_ran()}
				{/if}
			</span>
		</div>
	</div>

	<span
		class={cn("shrink-0 text-right text-[12.5px] font-semibold", state.tone)}
	>
		{trailing}
	</span>

	{#if row.system}
		<span
			class="grid h-11 w-8 shrink-0 place-items-center text-fg-faint"
			title={i18n.settings_managed_by_streamline()}
		>
			<Lock size={14} aria-hidden="true" />
			<span class="sr-only">{i18n.settings_managed_by_streamline()}</span>
		</span>
	{:else}
		<button
			type="button"
			onclick={() => onMenu(row)}
			aria-label={i18n.schedule_actions_for({ name: row.name })}
			class="grid h-11 w-11 shrink-0 place-items-center rounded-lg text-fg-muted transition active:bg-bg-hover"
		>
			<MoreHorizontal size={18} aria-hidden="true" />
		</button>
	{/if}
</div>
