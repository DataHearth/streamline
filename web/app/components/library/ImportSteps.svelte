<script lang="ts">
	import { Check, LoaderCircle } from "@lucide/svelte";
	import type { ImportStatus } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { status, series = false }: { status: ImportStatus; series?: boolean } =
		$props();

	type State = "done" | "current" | "pending";

	const STEPS = $derived([
		{ label: i18n.imports_discovery(), sub: i18n.imports_step_indexing() },
		{ label: i18n.imports_parsing(), sub: i18n.imports_matching_against({ provider: series ? "TVDB" : "TMDB" }) },
		{ label: i18n.common_review(), sub: i18n.imports_step_resolve() },
		{ label: i18n.imports_commit(), sub: i18n.imports_step_import() },
	]);

	// Map a scan status onto the four-stage pipeline. cancelled/failed freeze
	// at whatever was reached (no "current" highlight — the route renders a
	// dedicated failure banner for the reason).
	function statesFor(s: ImportStatus): State[] {
		switch (s) {
			case "running":
				return ["done", "current", "pending", "pending"];
			case "awaiting_review":
				return ["done", "done", "current", "pending"];
			case "committing":
				return ["done", "done", "done", "current"];
			case "completed":
				return ["done", "done", "done", "done"];
			case "cancelled":
			case "failed":
				return ["done", "done", "pending", "pending"];
		}
	}

	let states = $derived(statesFor(status));
	let live = $derived(status === "running" || status === "committing");

	// The phone strip names one step. A cancelled/failed scan has no "current",
	// so it falls back to the last stage actually reached — which is the thing
	// the status pill alone cannot say.
	let currentIndex = $derived(
		states.indexOf("current") !== -1
			? states.indexOf("current")
			: Math.max(0, states.lastIndexOf("done")),
	);
	// currentIndex is always within [0, STEPS.length): indexOf/lastIndexOf over
	// `states`, which is derived 1:1 from STEPS, clamped to 0 by Math.max.
	let current = $derived(STEPS[currentIndex]!);
</script>

<!-- Below md the four labels are single unbreakable words that overflow a 358px
     card, so the phone gets dots plus the current step's label and sub-label —
     the latter being exactly what the old strip hid at this width. -->
<div
	class="flex items-center gap-3 rounded-lg border border-border bg-bg-elevated px-4 py-3 md:hidden"
	aria-label={i18n.imports_progress()}
>
	<div class="flex shrink-0 items-center gap-1.5" aria-hidden="true">
		{#each STEPS as step, i (step.label)}
			{@const state = states[i]}
			<span
				class="rounded-full transition-colors {state === 'done'
					? 'h-2 w-2 bg-status-available'
					: state === 'current'
						? 'h-2.5 w-2.5 bg-accent ring-2 ring-accent-ring'
						: 'h-2 w-2 bg-border-strong'}"
			></span>
		{/each}
	</div>
	<div class="min-w-0 flex-1">
		<p class="truncate text-[13.5px] font-semibold tracking-[-0.01em] text-fg">
			{current.label}
		</p>
		<p class="mt-0.5 truncate text-[11.5px] text-fg-subtle">{current.sub}</p>
	</div>
	<span class="shrink-0 font-mono text-[10.5px] tabular-nums text-fg-faint">
		{currentIndex + 1}/{STEPS.length}
	</span>
</div>

<ol
	class="hidden items-stretch gap-0 rounded-lg border border-border bg-bg-elevated px-3 py-4 md:flex md:px-6"
	aria-label={i18n.imports_progress()}
>
	{#each STEPS as step, i (step.label)}
		{@const state = states[i]}
		<li
			class="flex flex-1 items-center gap-1.5 last:flex-none md:gap-2.5"
			aria-current={state === "current" ? "step" : undefined}
		>
			<span
				class="grid h-5 w-5 shrink-0 place-items-center rounded-full text-[11px] font-semibold transition-colors md:h-6 md:w-6 {state ===
				'done'
					? 'bg-status-available text-bg-deep'
					: state === 'current'
						? 'bg-accent text-fg-on-accent ring-2 ring-accent-ring'
						: 'border border-border bg-bg-card text-fg-faint'}"
			>
				{#if state === "done"}
					<Check size={13} aria-hidden="true" />
				{:else if state === "current" && live}
					<LoaderCircle
						size={13}
						class="motion-safe:animate-spin"
						aria-hidden="true"
					/>
				{:else}
					{i + 1}
				{/if}
			</span>

			<span class="min-w-0">
				<span
					class="block text-[12.5px] font-semibold leading-tight {state ===
					'pending'
						? 'text-fg-faint'
						: 'text-fg'}"
				>
					{step.label}
				</span>
				<span
					class="mt-0.5 hidden text-[10.5px] text-fg-subtle sm:block"
				>
					{step.sub}
				</span>
			</span>

			{#if i < STEPS.length - 1}
				<span
					class="mx-0.5 h-px flex-1 md:mx-1 {state === 'done'
						? 'bg-status-available/40'
						: 'bg-border'}"
					aria-hidden="true"
				></span>
			{/if}
		</li>
	{/each}
</ol>
