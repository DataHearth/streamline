<script lang="ts">
	import { cn } from "../../lib/cn";
	import type { StatusKind } from "./StatusPill.svelte";

	let {
		value,
		status = "downloading",
		height = 4,
		shimmer = false,
		label,
	}: {
		value?: number;
		status?: StatusKind;
		height?: 1 | 1.5 | 2 | 4;
		shimmer?: boolean;
		label?: string;
	} = $props();

	let indeterminate = $derived(value === undefined);
	let pct = $derived(
		value === undefined ? 0 : Math.max(0, Math.min(1, value)) * 100,
	);
</script>

<div
	class="track relative w-full overflow-hidden rounded-full bg-white/[0.06]"
	role="progressbar"
	aria-label={label ?? "Progress"}
	aria-valuenow={indeterminate ? undefined : Math.round(pct)}
	aria-valuemin={indeterminate ? undefined : 0}
	aria-valuemax={indeterminate ? undefined : 100}
	style:--c="var(--status-{status})"
	style:--h="{height}px"
	style:--pct="{pct}%"
>
	{#if indeterminate}
		<div
			class="indeterminate absolute inset-y-0 left-0 w-2/5 rounded-full motion-reduce:left-[30%]"
		></div>
	{:else}
		<div
			class={cn(
				"fill h-full rounded-full",
				shimmer && "relative overflow-hidden",
			)}
		>
			{#if shimmer}
				<span
					aria-hidden="true"
					class="shimmer absolute inset-0 bg-gradient-to-r from-transparent via-white/35 to-transparent"
				></span>
			{/if}
		</div>
	{/if}
</div>

<style>
	.track {
		height: var(--h);
	}
	.indeterminate {
		background-color: var(--c);
	}
	.fill {
		background-color: var(--c);
		transform: translateX(calc(var(--pct) - 100%));
		transition: transform 300ms cubic-bezier(0.16, 1, 0.3, 1);
	}
	@media (prefers-reduced-motion: no-preference) {
		.indeterminate {
			animation: progress-indeterminate 1.6s cubic-bezier(0.65, 0, 0.35, 1)
				infinite;
		}
		.shimmer {
			animation: progress-shimmer 1.6s linear infinite;
		}
	}
	@keyframes progress-indeterminate {
		0% {
			transform: translateX(-100%);
		}
		100% {
			transform: translateX(250%);
		}
	}
	@keyframes progress-shimmer {
		0% {
			transform: translateX(-100%);
		}
		100% {
			transform: translateX(100%);
		}
	}
</style>
