<script lang="ts">
	import type { Component } from "svelte";
	import { Check, LoaderCircle, X } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import type { OutcomePhase } from "@lib/outcome.svelte";

	// A button that answers in place: a spinner while it runs, green with a
	// check on success, red with a cross on failure. `error` (below md only, see
	// createOutcome) shows in a bubble above the button, over what is there and
	// never moving it; from md the reason is a toast, which announces itself. The
	// button stays one element through every phase, so the colour change animates.
	let {
		phase,
		label,
		pendingLabel,
		doneLabel,
		failedLabel,
		icon: Icon,
		error,
		size = "md",
		class: className = "",
		onclick,
	}: {
		phase: OutcomePhase;
		label: string;
		pendingLabel: string;
		doneLabel: string;
		failedLabel: string;
		icon: Component<{ size?: number; "aria-hidden"?: boolean | "true" }>;
		error?: string;
		// md: a modal footer's button. lg: a phone sheet's full-width one.
		size?: "md" | "lg";
		class?: string;
		onclick: () => void;
	} = $props();

	let iconSize = $derived(size === "lg" ? 17 : 15);
</script>

<div class={cn("relative", size === "lg" ? "w-full" : "inline-flex", className)}>
	<button
		type="button"
		disabled={phase === "pending" || phase === "done"}
		aria-busy={phase === "pending"}
		{onclick}
		class={cn(
			"inline-flex items-center justify-center gap-1.5 font-semibold text-fg-on-accent transition-colors duration-200",
			size === "lg"
				? "h-[50px] w-full gap-2 rounded-xl text-[16px] active:opacity-80"
				: "min-h-11 rounded-md px-4 text-sm lg:h-9 lg:min-h-0",
			phase === "done"
				? "bg-status-available"
				: phase === "failed"
					? "bg-status-failed"
					: "bg-accent hover:bg-accent-hover",
			phase === "pending" && "cursor-progress opacity-60",
		)}
	>
		{#if phase === "pending"}
			<LoaderCircle size={iconSize} class="animate-spin" aria-hidden="true" />
			{pendingLabel}
		{:else if phase === "done"}
			<Check size={iconSize} aria-hidden="true" />
			{doneLabel}
		{:else if phase === "failed"}
			<X size={iconSize} aria-hidden="true" />
			{failedLabel}
		{:else}
			<Icon size={iconSize} aria-hidden="true" />
			{label}
		{/if}
	</button>
	{#if error && phase !== "pending"}
		<p
			class={cn(
				"pointer-events-none absolute bottom-full z-10 mb-2.5 rounded-lg border border-status-failed/50 bg-bg-elevated px-3 py-2 text-[12.5px] leading-snug break-words text-status-failed shadow-lg shadow-black/40",
				size === "lg" ? "inset-x-0 text-center" : "right-0 w-max max-w-[min(20rem,calc(100vw-4rem))]",
			)}
		>
			{error}
			<span
				aria-hidden="true"
				class={cn(
					"absolute -bottom-[5px] size-2.5 rotate-45 border-r border-b border-status-failed/50 bg-bg-elevated",
					size === "lg" ? "left-1/2 -translate-x-1/2" : "right-6",
				)}
			></span>
		</p>
	{/if}
	<span class="sr-only" aria-live="polite">
		{phase === "done" ? doneLabel : phase === "failed" && error ? error : ""}
	</span>
</div>
