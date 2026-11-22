<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { Zap, Pause, Play, Pencil, TriangleAlert } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import { config, READONLY_HINT } from "@lib/config.svelte";
	import { createScheduleActions } from "@lib/schedule-actions.svelte";
	import { scheduleState } from "@lib/schedules-touch";
	import type { Schedule } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The three 28px icon buttons the desktop row carries, as words. Pause vs
	// Resume gets to be a label instead of a glyph you decode, and each target
	// is a full row instead of a 28px square with 4px of air around it.
	let {
		job,
		onClose,
		onEditInterval,
	}: {
		job: Schedule | null;
		onClose: () => void;
		onEditInterval: (s: Schedule) => void;
	} = $props();

	// Mounted with the page; open while it has a job. A watcher armed while closed
	// would take the phone's Back from the page.
	const actions = createScheduleActions();


	let state = $derived(job ? scheduleState(job) : null);
	let paused = $derived(job?.paused ?? false);
	let busy = $derived(
		actions.run.isPending ||
			actions.pause.isPending ||
			actions.resume.isPending,
	);

	function runNow() {
		if (!job) return;
		actions.run.mutate(job.name);
		onClose();
	}
	function toggle() {
		if (!job) return;
		if (paused) actions.resume.mutate(job.name);
		else actions.pause.mutate(job.name);
		onClose();
	}
	function edit() {
		if (!job) return;
		const target = job;
		onClose();
		onEditInterval(target);
	}

	const item =
		"flex w-full items-center gap-3.5 rounded-xl px-2 py-3 text-left transition active:bg-bg-hover disabled:opacity-40";
</script>

{#if job}
	<BottomSheet
		label={i18n.schedule_actions_for({ name: job.name })}
		{onClose}
		headerClass="gap-2.5"
		bodyClass="pt-1 pb-[max(env(safe-area-inset-bottom),18px)]"
	>
		{#snippet header()}
			<h2 class="min-w-0 truncate font-mono text-[14px] font-semibold text-fg">
				{job.name}
			</h2>
			{#if state}
				<span class={twMerge("shrink-0 text-[12px] font-semibold", state.tone)}>
					{state.label}
				</span>
			{/if}
		{/snippet}
		<!-- The desktop table hides the failure reason in a title attribute,
		     which a touch device has no way to show at all. -->
		{#if job.last_error}
			<div
				class="mb-3 flex gap-2.5 rounded-lg border border-status-failed/35 bg-status-failed/[0.07] px-3 py-2.5 text-status-failed"
			>
				<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<p class="min-w-0 break-words text-[12px] leading-relaxed">
					{job.last_error}
				</p>
			</div>
		{/if}

		<button
			type="button"
			disabled={job.running || busy}
			onclick={runNow}
			class={item}
		>
			<Zap size={18} class="shrink-0 text-accent-text" aria-hidden="true" />
			<span class="text-[15px] font-medium text-fg"
				>{i18n.schedule_run_now()}</span
			>
		</button>

		<button type="button" disabled={busy} onclick={toggle} class={item}>
			{#if paused}
				<Play
					size={18}
					class="shrink-0 text-status-available"
					aria-hidden="true"
				/>
			{:else}
				<Pause
					size={18}
					class="shrink-0 text-status-wanted"
					aria-hidden="true"
				/>
			{/if}
			<span class="text-[15px] font-medium text-fg">
				{paused ? i18n.schedule_resume() : i18n.schedule_pause()}
			</span>
		</button>

		<button
			type="button"
			disabled={config.readOnly}
			title={config.readOnly ? READONLY_HINT : null}
			onclick={edit}
			class={item}
		>
			<Pencil size={18} class="shrink-0 text-fg-muted" aria-hidden="true" />
			<span class="min-w-0">
				<span class="block text-[15px] font-medium text-fg"
					>{i18n.schedule_edit_interval()}</span
				>
				<span class="mt-0.5 block text-[12px] text-fg-subtle">
					{i18n.schedule_currently({ interval: job.interval })}
				</span>
			</span>
		</button>
	</BottomSheet>
{/if}
