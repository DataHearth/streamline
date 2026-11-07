<script lang="ts">
	import type { Component } from "svelte";
	import { Check, LoaderCircle, X } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import type { OutcomePhase } from "@lib/outcome.svelte";

	// A button that answers in place: a spinner while it runs, green with a
	// check on success, red with a cross on failure and the reason under it.
	// It stays one element through every phase, so the colour change animates.
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

<div class={cn("flex flex-col gap-1.5", size === "lg" ? "w-full" : "items-end", className)}>
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
			class={cn("max-w-xs break-words text-[12.5px] leading-snug text-status-failed", size === "lg" ? "max-w-none text-center" : "text-right")}
		>
			{error}
		</p>
	{/if}
	<span class="sr-only" aria-live="polite">
		{phase === "done" ? doneLabel : phase === "failed" ? (error ?? failedLabel) : ""}
	</span>
</div>
