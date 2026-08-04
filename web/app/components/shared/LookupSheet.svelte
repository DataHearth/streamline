<script lang="ts">
	import type { Snippet } from "svelte";
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { ChevronUp } from "@lucide/svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";

	// A bottom sheet with two detents, used by the touch add/request flow. The
	// peek is content-sized and only has to confirm you picked the right title;
	// dragging it up — or tapping the hint, for anyone not dragging — promotes
	// it to full height for the whole detail. Dragging the peek down dismisses.
	// Touch only: above md the flow stays a centred modal.
	type Props = {
		open: boolean;
		label: string;
		expanded?: boolean;
		// Tappable affordance under the footer, shown in the peek only.
		hint?: string;
		onClose: () => void;
		peek: Snippet;
		full: Snippet;
		footer?: Snippet;
	};
	let {
		open,
		label,
		expanded = $bindable(false),
		hint,
		onClose,
		peek,
		full,
		footer,
	}: Props = $props();

	// Drag thresholds, in px of travel from where the pointer went down.
	const PROMOTE = -46;
	const DEMOTE = 90;
	const DISMISS = 74;

	let dragY = $state(0);
	let dragging = $state(false);
	let startY = 0;

	// The detents are two heights, and a class swap between them wouldn't
	// animate — so the peek's natural height is measured and both detents are
	// driven as explicit px on a transitioned box. The observed element is the
	// content column, which is never height-constrained while collapsed, so it
	// still reports growth when the lazily-fetched detail lands.
	let contentEl = $state<HTMLDivElement | null>(null);
	let peekHeight = $state(0);
	let availableHeight = $state(0);

	// An upward drag grows the sheet from its pinned bottom edge. Translating it
	// instead would lift the whole box off the bottom of the screen and show the
	// page behind it. Only a downward drag — which is heading for demote or
	// dismiss — moves the box itself.
	let lift = $derived(!expanded && dragY < 0 ? -dragY : 0);
	let slide = $derived(dragY > 0 ? dragY : 0);
	// The detail renders as soon as the drag opens room for it, so the gesture
	// swaps peek for full rather than growing an empty gap.
	let showFull = $derived(expanded || lift > 0);
	let sheetHeight = $derived(
		expanded
			? availableHeight
			: peekHeight
				? Math.min(peekHeight + lift, availableHeight)
				: 0,
	);

	$effect(() => {
		if (!open || !contentEl) return;
		const el = contentEl;
		const measure = () => {
			if (showFull) return;
			const h = el.scrollHeight;
			if (h > 0 && Math.abs(h - peekHeight) > 1) peekHeight = h;
		};
		measure();
		const ro = new ResizeObserver(measure);
		ro.observe(el);
		return () => ro.disconnect();
	});

	function down(e: PointerEvent) {
		if (e.pointerType === "mouse" && e.button !== 0) return;
		dragging = true;
		startY = e.clientY;
		const el = e.currentTarget;
		if (el && el.setPointerCapture) el.setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragging) return;
		dragY = e.clientY - startY;
	}
	function up() {
		if (!dragging) return;
		dragging = false;
		const dy = dragY;
		dragY = 0;
		if (!expanded) {
			if (dy <= PROMOTE) expanded = true;
			else if (dy >= DISMISS) onClose();
		} else if (dy >= DEMOTE) {
			expanded = false;
		}
	}

	$effect(() => {
		if (!open) {
			expanded = false;
			dragY = 0;
			dragging = false;
			peekHeight = 0;
			return;
		}
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "Escape") return;
			e.stopPropagation();
			if (expanded) expanded = false;
			else onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

{#if open}
	<div
		class="fixed inset-0 z-[60] md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={label}
	>
		<button
			type="button"
			aria-label="Close"
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/60 backdrop-blur-[2px]"
		></button>

		<!-- Outer box owns the detent geometry; the inner one takes the drag
		     transform, so a swipe never fights the enter transition. -->
		<div
			transition:fly={{ y: 460, duration: 280, easing: cubicOut }}
			bind:clientHeight={availableHeight}
			class="absolute inset-x-0 bottom-0 top-9 flex flex-col justify-end"
		>
			<div
				class="flex max-h-full min-h-0 flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
				style:height={sheetHeight ? `${sheetHeight}px` : undefined}
				style:transform={slide === 0 ? undefined : `translateY(${slide}px)`}
				style:transition={dragging
					? "height 260ms cubic-bezier(0.2,0.8,0.2,1)"
					: "height 260ms cubic-bezier(0.2,0.8,0.2,1), transform 240ms cubic-bezier(0.2,0.8,0.2,1)"}
			>
				<!-- Natural height while collapsed — a stretched column would measure
				     the height it was given and hold it there. -->
				<div
					bind:this={contentEl}
					class="flex min-h-0 flex-col {showFull ? 'flex-1' : 'flex-none'}"
				>
				<div
					class="flex-none cursor-grab touch-none select-none pt-2.5 pb-1"
					onpointerdown={down}
					onpointermove={move}
					onpointerup={up}
					onpointercancel={up}
				>
					<span
						aria-hidden="true"
						class="mx-auto block h-1 w-9 rounded-full bg-border-strong"
					></span>
				</div>

				<div
					class="px-4 pb-4 {showFull
						? 'min-h-0 flex-1 overflow-y-auto overscroll-contain'
						: 'flex-none'}"
				>
					{#if showFull}
						{@render full()}
					{:else}
						{@render peek()}
					{/if}
				</div>

				{#if footer}
					<div
						class="flex-none px-4 pt-3 pb-[max(env(safe-area-inset-bottom),12px)] {showFull
							? 'border-t border-border'
							: ''}"
					>
						{@render footer()}
						{#if hint && !showFull}
							<button
								type="button"
								onclick={() => (expanded = true)}
								class="mt-2.5 flex w-full items-center justify-center gap-1.5 py-1 text-[13px] text-fg-faint transition hover:text-fg-muted"
							>
								<ChevronUp size={14} aria-hidden="true" />
								{hint}
							</button>
						{/if}
					</div>
				{/if}
				</div>
			</div>
		</div>
	</div>
{/if}
