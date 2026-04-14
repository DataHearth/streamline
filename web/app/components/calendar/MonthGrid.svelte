<script lang="ts">
	import { onDestroy, tick } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import {
		buildMonthGrid,
		eventsForDay,
		isSameDay,
		resolveWeekStart,
		weekdayLabels,
		type CalendarEvent,
	} from "../../lib/calendar";
	import EventDot from "./EventDot.svelte";

	let {
		year,
		month0,
		events,
	}: { year: number; month0: number; events: CalendarEvent[] } = $props();

	const weekStart = resolveWeekStart();
	const labels = weekdayLabels(weekStart);
	const today = new Date();
	const longDate = new Intl.DateTimeFormat(undefined, {
		weekday: "long",
		month: "long",
		day: "numeric",
	});

	let grid = $derived(buildMonthGrid(year, month0, weekStart));

	const VISIBLE = 3;
	const POP_W = 248;
	const GAP = 6;

	let openKey = $state<string | null>(null);
	let popEvents = $state<CalendarEvent[]>([]);
	let popLabel = $state("");
	let triggerEl: HTMLButtonElement | null = null;
	let popEl = $state<HTMLDivElement | null>(null);
	let popTop = $state(0);
	let popLeft = $state(0);

	function recompute() {
		if (!triggerEl) return;
		const r = triggerEl.getBoundingClientRect();
		if (
			r.bottom < 0 ||
			r.top > window.innerHeight ||
			r.right < 0 ||
			r.left > window.innerWidth
		) {
			close();
			return;
		}
		const h = popEl?.offsetHeight ?? 220;
		const below = r.bottom + GAP;
		// Flip above the trigger when it would spill past the viewport bottom.
		popTop =
			below + h > window.innerHeight - 8 && r.top - GAP - h > 8
				? r.top - GAP - h
				: below;
		popLeft = Math.min(
			Math.max(8, r.left),
			Math.max(8, window.innerWidth - POP_W - 8),
		);
	}

	async function open(
		key: string,
		dayEvents: CalendarEvent[],
		label: string,
		el: HTMLButtonElement,
	) {
		triggerEl = el;
		popEvents = dayEvents;
		popLabel = label;
		openKey = key;
		await tick();
		recompute();
		window.addEventListener("scroll", recompute, true);
		window.addEventListener("resize", recompute);
		const first = popEl?.querySelector<HTMLElement>("a");
		first?.focus();
	}

	function close() {
		if (openKey === null) return;
		openKey = null;
		window.removeEventListener("scroll", recompute, true);
		window.removeEventListener("resize", recompute);
		triggerEl?.focus();
		triggerEl = null;
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === "Escape") {
			e.preventDefault();
			close();
		}
	}

	function onDocPointer(e: MouseEvent) {
		const t = e.target as Node;
		if (popEl?.contains(t) || triggerEl?.contains(t)) return;
		close();
	}

	$effect(() => {
		if (openKey !== null) {
			document.addEventListener("mousedown", onDocPointer);
			document.addEventListener("keydown", onKey);
			return () => {
				document.removeEventListener("mousedown", onDocPointer);
				document.removeEventListener("keydown", onKey);
			};
		}
	});

	onDestroy(() => {
		window.removeEventListener("scroll", recompute, true);
		window.removeEventListener("resize", recompute);
	});

	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() {
				node.parentNode?.removeChild(node);
			},
		};
	}
</script>

<div class="rounded-lg border border-border bg-bg-elevated p-2 sm:p-3">
	<div
		class="grid grid-cols-7 gap-1.5 px-1 pb-2"
		aria-hidden="true"
	>
		{#each labels as label (label)}
			<span
				class="font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint"
			>
				{label}
			</span>
		{/each}
	</div>

	<div class="grid grid-cols-7 gap-1.5">
		{#each grid as week, w (w)}
			{#each week as cell (cell.date.toISOString())}
				{@const evs = eventsForDay(events, cell.date)}
				{@const isToday = isSameDay(cell.date, today)}
				{@const key = cell.date.toDateString()}
				<div
					class={cn(
						"flex min-h-[72px] flex-col gap-1 rounded-md border border-border p-1.5 sm:min-h-[92px] sm:p-2",
						!cell.inMonth && "opacity-40",
						isToday && "ring-2 ring-inset ring-accent",
					)}
				>
					<div class="flex items-center justify-between">
						<span
							class={cn(
								"font-mono text-[11px] font-semibold tabular",
								isToday ? "text-accent-text" : "text-fg-muted",
							)}
						>
							{cell.date.getDate()}
						</span>
					</div>

					{#each evs.slice(0, VISIBLE) as e (e.id)}
						<a
							href={e.href}
							title={e.subtitle ? `${e.title} · ${e.subtitle}` : e.title}
							style:--c="var(--status-{e.status})"
							class="chip flex items-center gap-1.5 overflow-hidden rounded bg-bg-card px-1.5 py-1 text-left text-[10.5px] text-fg transition-colors hover:bg-bg-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							<EventDot status={e.status} />
							<span class="truncate font-medium">{e.title}</span>
							{#if e.subtitle}
								<span class="shrink-0 font-mono text-[9px] text-fg-faint">
									{e.subtitle}
								</span>
							{/if}
						</a>
					{/each}

					{#if evs.length > VISIBLE}
						<button
							type="button"
							aria-haspopup="dialog"
							aria-expanded={openKey === key}
							aria-label="Show all {evs.length} releases on {longDate.format(
								cell.date,
							)}"
							onclick={(ev) =>
								open(
									key,
									evs,
									longDate.format(cell.date),
									ev.currentTarget,
								)}
							class="rounded px-1.5 text-left font-mono text-[9.5px] text-fg-subtle transition-colors hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							+{evs.length - VISIBLE} more
						</button>
					{/if}
				</div>
			{/each}
		{/each}
	</div>
</div>

{#if openKey !== null}
	<div
		bind:this={popEl}
		use:portal
		role="dialog"
		aria-label="Releases on {popLabel}"
		transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
		class="pop fixed z-50 flex max-h-[60vh] flex-col overflow-hidden rounded-md border border-border-strong bg-bg-elevated shadow-4"
		style:--pop-top="{popTop}px"
		style:--pop-left="{popLeft}px"
		style:--pop-w="{POP_W}px"
	>
		<div
			class="border-b border-border px-3 py-2 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint"
		>
			{popLabel}
		</div>
		<div class="flex flex-col gap-1 overflow-y-auto p-1.5">
			{#each popEvents as e (e.id)}
				<a
					href={e.href}
					title={e.subtitle ? `${e.title} · ${e.subtitle}` : e.title}
					style:--c="var(--status-{e.status})"
					class="chip flex items-center gap-2 overflow-hidden rounded bg-bg-card px-2 py-1.5 text-left text-[12px] text-fg transition-colors hover:bg-bg-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
				>
					<EventDot status={e.status} />
					<span class="truncate font-medium">{e.title}</span>
					{#if e.subtitle}
						<span class="shrink-0 font-mono text-[10px] text-fg-faint">
							{e.subtitle}
						</span>
					{/if}
				</a>
			{/each}
		</div>
	</div>
{/if}

<style>
	.chip {
		border: 1px solid var(--border);
		border-left: 2px solid var(--c);
	}
	.chip:hover {
		border-color: var(--border-strong);
		border-left-color: var(--c);
	}
	.pop {
		top: var(--pop-top);
		left: var(--pop-left);
		width: var(--pop-w);
	}
</style>
