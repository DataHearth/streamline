<script lang="ts">
	import {
		ArrowDown,
		ArrowUp,
		Check,
		Ellipsis,
		Pause,
		TriangleAlert,
		X,
	} from "@lucide/svelte";
	import { ringReading } from "../../lib/activity-touch";
	import type { StatusKind } from "../shared/StatusPill.svelte";

	// The leading mark of every touch row: an arc for progress, the status colour
	// for state, and a number only where a percentage is true. See ringReading.
	let {
		status,
		progress = 0,
		size = "md",
		label,
	}: {
		status: StatusKind;
		progress?: number;
		size?: "sm" | "md" | "lg";
		label?: string;
	} = $props();

	const DIM = { sm: 32, md: 40, lg: 46 };
	const STROKE = { sm: 2.5, md: 3, lg: 4 };
	const NUM = { sm: 10, md: 11, lg: 12 };
	const GLYPH = { sm: 11, md: 12, lg: 14 };

	let reading = $derived(ringReading(status));
	let value = $derived(Math.min(1, Math.max(0, progress ?? 0)));
	let arc = $derived(reading.mode === "full" ? 100 : Math.round(value * 100));
</script>

<div
	class="ring-wrap relative grid shrink-0 place-items-center"
	style:width="{DIM[size]}px"
	style:height="{DIM[size]}px"
	role="img"
	aria-label={label ?? `${status} ${arc}%`}
>
	<div
		class="ring absolute inset-0 rounded-full"
		class:spin={reading.mode === "spin"}
		style:--c="var(--status-{status})"
		style:--arc="{arc}%"
		style:--w="{STROKE[size]}px"
	></div>
	{#if reading.glyph === null}
		<span
			class="relative font-mono font-medium tabular-nums text-fg-muted"
			style:font-size="{NUM[size]}px"
		>
			{arc}
		</span>
	{:else}
		<span
			class="relative grid place-items-center"
			style:color={reading.glyph === "pause"
				? "var(--fg-muted)"
				: `var(--status-${status})`}
		>
			{#if reading.glyph === "check"}
				<Check size={GLYPH[size]} strokeWidth={2.6} aria-hidden="true" />
			{:else if reading.glyph === "cross"}
				<X size={GLYPH[size]} strokeWidth={2.6} aria-hidden="true" />
			{:else if reading.glyph === "pause"}
				<Pause size={GLYPH[size]} strokeWidth={2.4} aria-hidden="true" />
			{:else if reading.glyph === "up"}
				<ArrowUp size={GLYPH[size]} strokeWidth={2.6} aria-hidden="true" />
			{:else if reading.glyph === "down"}
				<ArrowDown size={GLYPH[size]} strokeWidth={2.6} aria-hidden="true" />
			{:else if reading.glyph === "alert"}
				<TriangleAlert size={GLYPH[size]} strokeWidth={2.4} aria-hidden="true" />
			{:else}
				<Ellipsis size={GLYPH[size]} strokeWidth={2.6} aria-hidden="true" />
			{/if}
		</span>
	{/if}
</div>

<style>
	/* Registered so the sweep can be transitioned: an unregistered custom
	   property has no type, and a conic-gradient stop built from one jumps. */
	@property --arc {
		syntax: "<percentage>";
		inherits: false;
		initial-value: 0%;
	}

	.ring {
		background: conic-gradient(var(--c) var(--arc), var(--surface) 0);
		/* Punching the middle out with a mask rather than stacking a filled
		   circle on top keeps the ring transparent, so it sits on the row's
		   surface, the sheet's, or a table cell's without carrying a background
		   colour of its own. */
		-webkit-mask: radial-gradient(
			farthest-side,
			transparent calc(100% - var(--w)),
			#000 calc(100% - var(--w))
		);
		mask: radial-gradient(
			farthest-side,
			transparent calc(100% - var(--w)),
			#000 calc(100% - var(--w))
		);
		transition: --arc 600ms var(--ease);
	}

	/* The queue repolls every 2 s, so the arc would step in 2 s jumps. It eases
	   between samples instead — slightly behind the truth, but the progress bars
	   next to it already shimmer, and a jumping ring would be the odd one out. */
	.ring.spin {
		background: conic-gradient(var(--c) 0 22%, var(--surface) 0);
		animation: ring-spin 1.1s linear infinite;
		transition: none;
	}

	@keyframes ring-spin {
		to {
			transform: rotate(1turn);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.ring {
			transition: none;
		}
		.ring.spin {
			animation-duration: 2.4s;
		}
	}
</style>
