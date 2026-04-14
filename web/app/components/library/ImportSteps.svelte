<script lang="ts">
	import { Check, LoaderCircle } from "@lucide/svelte";
	import type { ImportStatus } from "../../lib/types";

	let { status }: { status: ImportStatus } = $props();

	type State = "done" | "current" | "pending";

	const STEPS = [
		{ label: "Discovery", sub: "Indexing files" },
		{ label: "Parsing", sub: "Matching against TMDB" },
		{ label: "Review", sub: "Resolve matches" },
		{ label: "Commit", sub: "Import into library" },
	] as const;

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
</script>

<ol
	class="flex items-stretch gap-0 rounded-lg border border-border bg-bg-elevated px-4 py-4 md:px-6"
	aria-label="Import progress"
>
	{#each STEPS as step, i (step.label)}
		{@const state = states[i]}
		<li
			class="flex flex-1 items-center gap-2.5 last:flex-none"
			aria-current={state === "current" ? "step" : undefined}
		>
			<span
				class="grid h-6 w-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold transition-colors {state ===
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
					class="mx-1 h-px flex-1 {state === 'done'
						? 'bg-status-available/40'
						: 'bg-border'}"
					aria-hidden="true"
				></span>
			{/if}
		</li>
	{/each}
</ol>
