<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X, Zap, Pause, Play, Pencil, TriangleAlert } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { createScheduleActions } from "../../lib/schedule-actions.svelte";
	import { scheduleState } from "../../lib/schedules-touch";
	import type { Schedule } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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

	const actions = createScheduleActions();

	$effect(() => {
		if (!job) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

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
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.schedule_actions_for({ name: job.name })}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center gap-2.5 px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="min-w-0 truncate font-mono text-[14px] font-semibold text-fg">
					{job.name}
				</h2>
				{#if state}
					<span class={cn("shrink-0 text-[12px] font-semibold", state.tone)}>
						{state.label}
					</span>
				{/if}
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="ml-auto grid h-9 w-9 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2 pt-1"
			>
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
			</div>

			<div class="pb-[max(env(safe-area-inset-bottom),10px)]"></div>
		</div>
	</div>
{/if}
