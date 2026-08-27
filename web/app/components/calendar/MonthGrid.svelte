<script lang="ts">
	import { onDestroy, tick } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import {
		buildMonthGrid,
		dotToken,
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

	// From lg the grid is height-clamped to the viewport (the page does not
	// scroll), so how many chips a cell can hold is a measured quantity rather
	// than a constant. Below that the cells keep their 92px minimum and three.
	const MAX_VISIBLE = 3;
	// Measured, not guessed: a chip is 21.75px tall plus the 4px flex gap, and
	// the header is the cell's 12px of padding plus a 16.5px date row.
	const CHIP_H = 26;
	const HEAD_H = 29;
	let weeksEl = $state<HTMLDivElement | null>(null);
	let cellH = $state(0);

	$effect(() => {
		const first = weeksEl?.firstElementChild as HTMLElement | null;
		if (!first) return;
		const ro = new ResizeObserver(() => (cellH = first.clientHeight));
		ro.observe(first);
		cellH = first.clientHeight;
		return () => ro.disconnect();
	});

	// Date row plus the cell's own padding come off the top before chips fit.
	let chipRoom = $derived(Math.max(0, cellH - HEAD_H));
	// An overflowing day spends one of these rows on the "+N" button (it is
	// shorter than a chip, so a whole row is the conservative reservation): a
	// cell with room for three shows two releases and "+N". Budgeting it as a
	// height subtracted from the room was off by a slot — integer division turns
	// any shortfall into a lost chip — so a two-chip cell showed one and "+1".
	let fits = $derived(Math.min(MAX_VISIBLE, Math.floor(chipRoom / CHIP_H)));
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

<!-- md and up only: below that the phone renders DotGrid, so the small-screen
     variants this used to carry are gone. -->
<div
	class="flex flex-col rounded-lg border border-border bg-bg-elevated p-2 lg:h-full lg:min-h-0"
>
	<div
		class="grid flex-none grid-cols-7 gap-1.5 px-1 pb-2"
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

	<div
		bind:this={weeksEl}
		class="weeks grid grid-cols-7 gap-1.5 lg:min-h-0 lg:flex-1"
		style:--weeks={grid.length}
	>
		{#each grid as week, w (w)}
			{#each week as cell (cell.date.toISOString())}
				{@const evs = eventsForDay(events, cell.date)}
				{@const vis =
					evs.length <= fits ? evs.length : Math.max(0, fits - 1)}
				{@const isToday = isSameDay(cell.date, today)}
				{@const key = cell.date.toDateString()}
				<div
					class={cn(
						"flex min-h-[92px] flex-col gap-1 overflow-hidden rounded-md border border-border p-1.5 lg:min-h-0",
						!cell.inMonth && "opacity-40",
						isToday && "ring-2 ring-inset ring-accent",
					)}
				>
					<div class="flex flex-none items-center justify-between">
						<span
							class={cn(
								"font-mono text-[11px] font-semibold tabular",
								isToday ? "text-accent-text" : "text-fg-muted",
							)}
						>
							{cell.date.getDate()}
						</span>
					</div>

					<!-- The chip carries the title and nothing else. A `shrink-0`
					     "S01E06" plus a dot claimed ~45px of a 74px chip, and the
					     title — the only flexible child — absorbed the whole
					     shortfall: every episode rendered one character. Kind is
					     already the 2px coloured left border, and the episode number
					     is in the tooltip, the +N popover and the day panel. -->
					{#each evs.slice(0, vis) as e (e.id)}
						<a
							href={e.href}
							title={e.subtitle ? `${e.title} · ${e.subtitle}` : e.title}
							style:--c="var(--kind-{dotToken(e)})"
							class="chip block flex-none truncate rounded bg-bg-card px-1.5 py-0.5 text-left text-[10.5px] font-medium text-fg transition-colors hover:bg-bg-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							{e.title}
						</a>
					{/each}

					{#if evs.length > vis}
						<button
							type="button"
							aria-haspopup="dialog"
							aria-expanded={openKey === key}
							aria-label="Show the {evs.length -
								vis} remaining releases on {longDate.format(cell.date)}"
							onclick={(ev) =>
								open(
									key,
									evs.slice(vis),
									longDate.format(cell.date),
									ev.currentTarget,
								)}
							class="flex-none rounded px-1.5 py-0.5 text-left font-mono text-[10px] text-fg-subtle transition-colors hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
						>
							{i18n.calendar_more_count({ count: evs.length - vis })}
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
		aria-label={i18n.calendar_releases_on({ day: popLabel })}
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
					style:--c="var(--kind-{dotToken(e)})"
					class="chip flex items-center gap-2 overflow-hidden rounded bg-bg-card px-2 py-1.5 text-left text-[12px] text-fg transition-colors hover:bg-bg-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
				>
					<EventDot kind={dotToken(e)} />
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
	/* Height-clamped from lg: the weeks share whatever the viewport leaves, so a
	   five- and a six-week month both fit without the page scrolling. */
	@media (min-width: 1024px) {
		.weeks {
			grid-template-rows: repeat(var(--weeks), minmax(0, 1fr));
		}
	}
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
