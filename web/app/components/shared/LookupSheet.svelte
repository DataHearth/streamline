<script lang="ts">
	import type { Snippet } from "svelte";
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { ChevronUp } from "@lucide/svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// A bottom sheet with two detents, used by the touch add/request flow. The
	// peek is content-sized and only has to confirm you picked the right title;
	// dragging it up — or tapping the hint, for anyone not dragging — promotes
	// it to full height. The peek stays rendered in both detents and `full` is
	// appended below it, so promoting adds detail under the header instead of
	// swapping one header for another. Dragging the peek down dismisses.
	// Touch only: above md the flow stays a centred modal.
	type Props = {
		open: boolean;
		label: string;
		expanded?: boolean;
		// Tappable affordance under the footer, shown in the peek only.
		hint?: string;
		onClose: () => void;
		// Gets the detent so it can relax clamps once the sheet is fully open.
		peek: Snippet<[boolean]>;
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

	// Below this the gesture is still a tap. Capturing the pointer any earlier
	// would retarget the click and swallow taps on the buttons inside the sheet.
	const SLOP = 6;

	// dragY is deliberately NOT reactive: mobile Gecko showed ~80ms stalls when
	// every pointermove went through a framework flush, so the hot path writes
	// element.style.transform directly and reactivity only handles the detents.
	let dragY = 0;
	let dragging = $state(false);
	// Flipped once when a drag first opens room above the peek, so the footer
	// restyles mid-gesture without dragY being reactive.
	let dragLift = $state(false);
	let startY = 0;
	let pointerId: number | null = null;
	// Mobile Gecko delivers pointermove at a few Hz during a claimed touch drag
	// while touchmove flows at input rate, so the first touchmove takes over as
	// the movement source and pointermove is ignored for the rest of the
	// gesture. Pointer events still own down/up and the mouse path.
	let touchDriven = false;

	let sheetEl = $state<HTMLDivElement | null>(null);
	let footerEl = $state<HTMLDivElement | null>(null);
	let scrollEl = $state<HTMLDivElement | null>(null);
	// While the detail sits at its top, the scroll container advertises
	// pan-down (finger-up scrolling) only. APZ then knows a downward gesture
	// cannot scroll and streams it to us immediately — without this, Gecko
	// runs scroll-intent detection first and every demote-swipe starts with a
	// dead zone (bugzilla 913942).
	let atTop = $state(true);
	let peekHeight = $state(0);
	let handleHeight = $state(0);
	let footerHeight = $state(0);
	let availableHeight = $state(0);

	// Both detents are the same full-height box at a different translate3d, so
	// the drag and the settle are pure transform. Driving them as a height
	// instead relayouts the whole detail on every frame, which a phone shows as
	// jitter. will-change is load-bearing on Gecko: ActiveLayerTracker refuses
	// to layerize a frequently-repainted JS-driven transform, so without the
	// hint mobile Firefox repaints the sheet every frame at a locked ~30fps
	// (measured p50 33ms vs 8ms). Never reintroduce backdrop-filter on the
	// scrim either — WebRender re-blurs on the mobile GPU every composited
	// frame (bugzilla 1731965/1798592/1809738; measured ~80ms stalls).
	let collapsedOffset = $derived(
		peekHeight
			? Math.max(0, availableHeight - peekHeight - handleHeight - footerHeight)
			: availableHeight,
	);
	let offset = $derived(expanded ? 0 : collapsedOffset);
	// Styling only. `full` is permanently laid out below the peek and is simply
	// off-screen while collapsed, so nothing has to render mid-gesture.
	let lifted = $derived(expanded || dragLift);
	let settled = $derived(peekHeight > 0 && !dragging);

	// The footer stays pinned while the sheet slides behind it, and only rides
	// along once the sheet is past its peek and heading off the bottom.
	function applyDrag() {
		const base = expanded ? 0 : collapsedOffset;
		const off = Math.min(Math.max(base + dragY, 0), availableHeight);
		if (!expanded && off < collapsedOffset) dragLift = true;
		if (sheetEl) sheetEl.style.transform = `translate3d(0,${off}px,0)`;
		if (footerEl)
			footerEl.style.transform = `translate3d(0,${Math.max(0, off - collapsedOffset)}px,0)`;
	}

	function ownsGesture(dy: number) {
		if (!expanded) return true;
		// The expanded detail scrolls. The sheet only takes a downward gesture
		// there once the content has nothing left to scroll back up to.
		return !scrollEl || (scrollEl.scrollTop <= 0 && dy > 0);
	}

	function drive(dy: number) {
		if (!dragging) {
			if (Math.abs(dy) < SLOP) return;
			dragging = true;
		}
		dragY = dy;
		applyDrag();
	}

	// touchmove has to be non-passive and prevented on the very first move the
	// sheet claims: once the browser starts scrolling the detail it keeps the
	// gesture and fires pointercancel, so a downward swipe from the synopsis
	// would scroll nothing and demote nothing. touch-action alone cannot
	// express "mine only when scrolled to the top".
	$effect(() => {
		const el = sheetEl;
		if (!el) return;
		const claim = (e: TouchEvent) => {
			if (pointerId === null) return;
			const touch = e.touches[0];
			if (!touch) return;
			const dy = touch.clientY - startY;
			if (!dragging && !ownsGesture(dy)) return;
			if (e.cancelable) e.preventDefault();
			touchDriven = true;
			drive(dy);
		};
		el.addEventListener("touchmove", claim, { passive: false });
		return () => el.removeEventListener("touchmove", claim);
	});

	function down(e: PointerEvent) {
		if (e.pointerType === "mouse" && e.button !== 0) return;
		if (expanded && scrollEl?.contains(e.target as Node) && scrollEl.scrollTop > 0)
			return;
		pointerId = e.pointerId;
		startY = e.clientY;
		touchDriven = false;
	}
	function move(e: PointerEvent) {
		if (e.pointerId !== pointerId || touchDriven) return;
		const dy = e.clientY - startY;
		if (!dragging) {
			if (Math.abs(dy) < SLOP || !ownsGesture(dy)) return;
			dragging = true;
			sheetEl?.setPointerCapture(e.pointerId);
		}
		dragY = dy;
		applyDrag();
	}
	function up() {
		pointerId = null;
		if (!dragging) return;
		dragging = false;
		const dy = dragY;
		dragY = 0;
		dragLift = false;
		if (!expanded) {
			if (dy <= PROMOTE) expanded = true;
			else if (dy >= DISMISS) {
				onClose();
				return;
			}
		} else if (dy >= DEMOTE) {
			expanded = false;
			// Peek and detail scroll as one column, so a demote from halfway down
			// the detail would otherwise leave the peek scrolled off the top.
			if (scrollEl) scrollEl.scrollTop = 0;
		}
		// A failed gesture leaves the manual mid-drag transform behind and the
		// reactive value hasn't changed, so Svelte won't rewrite it. Restore by
		// hand on the next frame, when the settle transition is armed again.
		requestAnimationFrame(() => applyDrag());
	}

	$effect(() => {
		if (!open) {
			expanded = false;
			dragY = 0;
			dragging = false;
			dragLift = false;
			pointerId = null;
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
	<div class="fixed inset-0 z-[60] md:hidden">
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/60"
		></button>

		<!-- Spans the screen, so it has to stay click-through or it would eat
		     every tap meant for the backdrop above the sheet. -->
		<div
			transition:fly={{ y: 460, duration: 280, easing: cubicOut }}
			bind:clientHeight={availableHeight}
			class="pointer-events-none absolute inset-x-0 bottom-0 top-9 overflow-hidden"
		>
			<div
				bind:this={sheetEl}
				role="dialog"
				aria-modal="true"
				aria-label={label}
				tabindex="-1"
				onpointerdown={down}
				onpointermove={move}
				onpointerup={up}
				onpointercancel={up}
				ontouchend={up}
				ontouchcancel={up}
				style:transform="translate3d(0,{offset}px,0)"
				style:will-change="transform"
				style:padding-bottom="{footerHeight}px"
				style:transition={settled
					? "transform 260ms cubic-bezier(0.2,0.8,0.2,1)"
					: "none"}
				class="pointer-events-auto absolute inset-x-0 top-0 flex h-full select-none flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4 {expanded
					? ''
					: 'touch-none'}"
			>
				<div
					bind:clientHeight={handleHeight}
					class="flex-none cursor-grab touch-none pt-2.5 pb-1"
				>
					<span
						aria-hidden="true"
						class="mx-auto block h-1 w-9 rounded-full bg-border-strong"
					></span>
				</div>

				<!-- Peek and detail are one scrolling column, and the detail is
				     permanently laid out rather than rendered on promote: a display
				     flip would run layout of the whole detail on the exact frame a
				     drag starts, a 40-80ms hitch per gesture on phones. While
				     collapsed the detail begins exactly where the footer does, so
				     the opaque footer and the overlay's clip hide it without any
				     per-frame work. -->
				<div
					bind:this={scrollEl}
					onscroll={() => (atTop = (scrollEl?.scrollTop ?? 0) <= 0)}
					style:touch-action={atTop ? "pan-down" : "pan-y"}
					class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4"
				>
					<!-- pb-4 sits on the measured block, not the column: it is part of
					     what the collapsed detent has to make room for. -->
					<div bind:clientHeight={peekHeight} class="pb-4">
						{@render peek(expanded)}
					</div>
					{@render full()}
				</div>
			</div>

			{#if footer}
				<!-- Outside the transformed box: it belongs to the bottom of the
				     screen, not to the sheet, so the sheet can be a pure transform. -->
				<div
					bind:this={footerEl}
					bind:clientHeight={footerHeight}
					style:transform="translate3d(0,{offset > collapsedOffset ? offset - collapsedOffset : 0}px,0)"
					style:will-change="transform"
					style:transition={settled
						? "transform 260ms cubic-bezier(0.2,0.8,0.2,1)"
						: "none"}
					class="pointer-events-auto absolute inset-x-0 bottom-0 bg-bg-elevated px-4 pt-3 pb-[max(env(safe-area-inset-bottom),12px)] {lifted
						? 'border-t border-border'
						: ''}"
				>
					{@render footer()}
					{#if hint && !lifted}
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
{/if}
