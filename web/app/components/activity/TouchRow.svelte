<script lang="ts" module>
	import type { LucideIcon } from "@lucide/svelte";

	// What a sideways swipe on the row does. `tone` is a CSS colour — the
	// action's status token — painted under the row as it slides away.
	export type RowSwipe = {
		label: string;
		icon: LucideIcon;
		tone: string;
		onSwipe: () => void;
	};
</script>

<script lang="ts">
	import { Check, ChevronRight } from "@lucide/svelte";
	import ProgressRing from "./ProgressRing.svelte";
	import { cn } from "@lib/cn";
	import { longPress } from "@lib/long-press";
	import { swipeRow } from "@lib/swipe-row";
	import type { MetaLine } from "@lib/activity-touch";
	import type { StatusKind } from "@components/shared/StatusPill.svelte";

	// One row shape for all three touch views. Only the third line differs
	// between Queue, History and Torrents — the ring, title and release line are
	// the same object every time.
	let {
		status,
		progress = 0,
		title,
		release,
		meta,
		badge,
		placeholderTitle = false,
		href,
		onOpen,
		onResolve,
		resolveLabel,
		selected,
		onLongPress,
		swipeStart,
		swipeEnd,
	}: {
		status: StatusKind;
		progress?: number;
		title: string;
		release: string;
		meta: MetaLine;
		// "untracked" on a torrent with no library item behind it.
		badge?: string;
		// A magnet that hasn't resolved has no name yet, so the title is standing
		// in for one and shouldn't read as the release's own.
		placeholderTitle?: boolean;
		// A row that navigates rather than opening a sheet — the dashboard's
		// queue rows, which have no detail surface of their own. Routify
		// intercepts the click, so an <a> is all it takes.
		href?: string;
		onOpen?: () => void;
		// A held download is the only queue state whose next move belongs to a
		// person, so its row carries the action instead of a chevron. Given this,
		// the row becomes a div with a button inside rather than a button in a
		// button, and tapping the text still opens the detail.
		onResolve?: () => void;
		resolveLabel?: string;
		// Defined only while the list is selecting: the row is then a toggle (the
		// page passes a toggling onOpen) and shows a check where the chevron was.
		// Resolve and href stand down, so a tap mid-triage never leaves the set.
		selected?: boolean;
		// Press and hold: how a phone starts a selection.
		onLongPress?: () => void;
		// Swipe right runs swipeStart, swipe left swipeEnd. A shortcut only:
		// every one of them is also a labelled button in the row's sheet. They
		// stand down while selecting, where a stray swipe would act on one row
		// in the middle of choosing several.
		swipeStart?: RowSwipe;
		swipeEnd?: RowSwipe;
	} = $props();

	let selecting = $derived(selected !== undefined);
	let swipeable = $derived(!selecting && !!(swipeStart || swipeEnd));
	let offset = $state(0);
	let settling = $state(false);
	let armed = $state(false);
	// Which side is showing, held through the spring back so the action stays
	// under the row until it has covered it again.
	let side = $state<"start" | "end" | null>(null);
	let revealed = $derived(side === "start" ? swipeStart : side === "end" ? swipeEnd : undefined);

	const rowClass =
		"flex w-full items-center gap-3 px-3 py-2.5 text-left transition active:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-ring";
	const divider = "border-b border-border last:border-b-0";
</script>

{#snippet lead()}
	<ProgressRing {status} {progress} />

	<span class="min-w-0 flex-1">
		<span class="flex min-w-0 items-center gap-1.5">
			<span
				class={cn(
					"truncate text-sm font-semibold tracking-[-0.01em]",
					placeholderTitle ? "italic text-fg-muted" : "text-fg",
				)}
			>
				{title}
			</span>
			{#if badge}
				<span
					class="shrink-0 rounded-full border border-border px-1.5 py-px text-[8.5px] font-semibold uppercase tracking-wide text-fg-subtle"
				>
					{badge}
				</span>
			{/if}
		</span>
		<span class="block truncate font-mono text-[11px] text-fg-subtle">
			{release}
		</span>
		<span
			class="block truncate font-mono text-[11px]"
			style:color={meta.color ?? "var(--fg-muted)"}
		>
			{meta.text || "—"}
		</span>
	</span>

{/snippet}

{#snippet body()}
	{@render lead()}
	{#if selecting}
		<span
			class={cn(
				"grid h-[18px] w-[18px] shrink-0 place-items-center rounded border",
				selected
					? "border-accent bg-accent text-fg-on-accent"
					: "border-border-strong bg-bg text-transparent",
			)}
			aria-hidden="true"
		>
			<Check size={12} strokeWidth={3} />
		</span>
	{:else}
		<ChevronRight size={15} class="shrink-0 text-fg-faint" aria-hidden="true" />
	{/if}
{/snippet}

{#snippet row(cls: string)}
{#if selecting}
	<button
		type="button"
		onclick={onOpen}
		aria-pressed={selected}
		class={cn(cls, selected && "bg-accent-soft")}
	>
		{@render body()}
	</button>
{:else if onResolve}
	<div
		class={cls}
		use:longPress={{ onLongPress: () => onLongPress?.(), disabled: !onLongPress }}
	>
		<button
			type="button"
			onclick={onOpen}
			class="flex min-w-0 flex-1 items-center gap-3 rounded-sm text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			{@render lead()}
		</button>
		<button
			type="button"
			onclick={onResolve}
			class="pill-action inline-flex h-11 shrink-0 items-center rounded-md px-4 text-[13px] font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			style:--c="var(--status-{status})"
		>
			{resolveLabel}
		</button>
	</div>
{:else if href}
	<a {href} class={cls}>{@render body()}</a>
{:else}
	<button
		type="button"
		onclick={onOpen}
		class={cls}
		use:longPress={{ onLongPress: () => onLongPress?.(), disabled: !onLongPress }}
	>
		{@render body()}
	</button>
{/if}
{/snippet}

{#if swipeable}
	<!-- The divider moves to the wrapper: `last:` has to see the list's own
	     children, and the row inside is always its wrapper's last. -->
	<div class={cn("relative overflow-hidden", divider)}>
		{#if revealed}
			<div
				aria-hidden="true"
				class={cn(
					"absolute inset-0 flex items-center gap-2 px-5 text-[13px] font-semibold",
					side === "start" ? "justify-start" : "justify-end",
				)}
				style:background-color={armed
					? revealed.tone
					: `color-mix(in oklab, ${revealed.tone} 28%, var(--bg-elevated))`}
				style:color={armed ? "var(--bg-deep)" : revealed.tone}
			>
				<revealed.icon
					size={20}
					class={cn("shrink-0 transition-transform duration-150", armed && "scale-110")}
				/>
				{revealed.label}
			</div>
		{/if}
		<div
			class="relative bg-bg-elevated"
			style:transform={offset ? `translateX(${offset}px)` : undefined}
			style:transition={settling ? "transform 220ms var(--ease)" : "none"}
			ontransitionend={() => {
				settling = false;
				if (offset === 0) side = null;
			}}
			use:swipeRow={{
				start: !!swipeStart,
				end: !!swipeEnd,
				disabled: !swipeable,
				onOffset: (dx, settle) => {
					settling = settle;
					offset = dx;
					if (dx !== 0) side = dx > 0 ? "start" : "end";
				},
				onArmed: (on) => (armed = on),
				onCommit: (side) => (side === "start" ? swipeStart : swipeEnd)?.onSwipe(),
			}}
		>
			{@render row(rowClass)}
		</div>
	</div>
{:else}
	{@render row(cn(rowClass, divider))}
{/if}

<style>
	/* Filled with the row's own status colour, dark text on it — the same
	   treatment StatusPill's solid variant uses, and for the same reason: the
	   utility class for a token added this late may not be generated. */
	.pill-action {
		background-color: var(--c);
		color: var(--bg-deep);
	}
	.pill-action:hover {
		filter: brightness(1.08);
	}
</style>
