<script lang="ts">
	import type { Snippet } from "svelte";
	import { onDestroy, tick, untrack } from "svelte";

	// The app's one dropdown surface. Everything that opens a list under a
	// trigger renders through this — <Select>, the library toolbars' facet and
	// sort menus — so the surface, the placement and flip, the dismissal rules
	// and the motion are stated once instead of per caller. The caller owns its
	// trigger and its open state; this owns everything below the trigger.
	type Props = {
		open: boolean;
		// The element the menu is placed against, and the one element a press on
		// is not "outside". A press on any OTHER trigger is outside: this closes,
		// and that trigger's own click opens its menu.
		anchor: HTMLElement | null;
		onClose: () => void;
		// Which edge of the anchor the menu lines up with.
		align?: "start" | "end";
		// Width := the anchor's, for a menu that reads as the field it drops from.
		// Otherwise the menu is as wide as its content, floored by minWidth.
		matchAnchor?: boolean;
		minWidth?: string;
		role?: "listbox" | "menu";
		ariaLabel?: string;
		children: Snippet;
	};

	let {
		open,
		anchor,
		onClose,
		align = "start",
		matchAnchor = false,
		minWidth,
		role = "listbox",
		ariaLabel,
		children,
	}: Props = $props();

	let menuEl = $state<HTMLDivElement | null>(null);
	let listEl = $state<HTMLUListElement | null>(null);
	let menuTop = $state(0);
	let menuLeft = $state(0);
	let menuWidth = $state(0);
	let flipped = $state(false);
	// The list's own height budget, from the space on the side it opened to. A
	// fixed cap scrolled six 44px rows in a window with room for twenty.
	let maxH = $state(0);
	// Placement happens a tick after mount, so the menu is held invisible until
	// it has one — otherwise the first frame paints at the top-left corner.
	let placed = $state(false);
	// The surface outlives `open` by one animation so it can close on screen.
	// `shown` is what mounts it; `closing` is what runs the outro.
	let shown = $state(false);
	let closing = $state(false);
	let closeTimer: ReturnType<typeof setTimeout> | null = null;
	let observer: ResizeObserver | null = null;
	const GAP = 8;
	const PAD = 8;
	const MIN_H = 176;
	// The list's own py-1.
	const LIST_PAD = 8;
	const OUT_MS = 120;

	$effect(() => {
		// `open` is the only dependency: the two flags below are written here, and
		// reading them reactively would make this effect chase itself.
		const isOpen = open;
		const wasShown = untrack(() => shown);
		const wasClosing = untrack(() => closing);
		if (isOpen) {
			if (closeTimer) clearTimeout(closeTimer);
			closeTimer = null;
			closing = false;
			shown = true;
		} else if (wasShown && !wasClosing) {
			// setTimeout, not a Svelte outro: an outro completes on
			// requestAnimationFrame, which never fires in an inactive tab or a hidden
			// preview frame, and a menu that never finished closing stayed mounted
			// over the page for good. A timer fires either way, and the closing
			// surface stops taking pointer events immediately, so the worst a missed
			// one can do is leave something invisible and inert.
			closing = true;
			closeTimer = setTimeout(() => {
				shown = false;
				closing = false;
				closeTimer = null;
			}, OUT_MS);
		}
	});

	function recompute() {
		if (!anchor) return;
		const r = anchor.getBoundingClientRect();
		// Scrolled out of view: the anchor is gone, so the menu goes with it.
		if (
			r.bottom < 0 ||
			r.top > window.innerHeight ||
			r.right < 0 ||
			r.left > window.innerWidth
		) {
			onClose();
			return;
		}
		menuWidth = r.width;
		const w = matchAnchor ? r.width : (menuEl?.offsetWidth ?? 0);
		// Keep the menu inside the viewport horizontally — an end-aligned menu on
		// a trigger near the right edge would otherwise hang off it.
		menuLeft = Math.max(
			PAD,
			Math.min(align === "end" ? r.right - w : r.left, window.innerWidth - w - PAD),
		);
		// Flip above the anchor when the menu will not fit below it: any trigger
		// in a modal footer, or a toolbar sitting low in a short window.
		// Measured on the list's scroll height, not the rendered box: the box is
		// already clamped by the budget this decides, so reading it back would let
		// the flip chase its own result.
		const h = listEl ? listEl.scrollHeight + LIST_PAD : (menuEl?.offsetHeight ?? 0);
		const below = window.innerHeight - r.bottom - GAP - PAD;
		const above = r.top - GAP - PAD;
		flipped = h > below && above > below;
		// Never below a floor: a cramped window gets a short scrolling list rather
		// than a sliver.
		maxH = Math.max(MIN_H, flipped ? above : below);
		menuTop = flipped ? Math.max(PAD, r.top - GAP - Math.min(h, maxH)) : r.bottom + GAP;
		placed = true;
	}

	function teardown() {
		observer?.disconnect();
		observer = null;
		window.removeEventListener("scroll", recompute, true);
		window.removeEventListener("resize", recompute);
	}
	$effect(() => {
		if (!open) return;
		placed = false;
		let alive = true;
		void tick().then(() => {
			if (!alive) return;
			recompute();
			// The portalled menu is not at its final size one tick after mount —
			// the browser Tailwind JIT has not emitted its utilities yet — and both
			// the flip and the end alignment are computed from that size. Re-measure
			// whenever it settles; this also covers webfont load and option changes.
			if (menuEl && typeof ResizeObserver !== "undefined") {
				observer = new ResizeObserver(() => recompute());
				observer.observe(menuEl);
			}
		});
		window.addEventListener("scroll", recompute, true);
		window.addEventListener("resize", recompute);
		const onDown = (e: MouseEvent) => {
			const t = e.target as Node;
			if (menuEl?.contains(t)) return;
			if (anchor?.contains(t)) return;
			onClose();
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "Escape") return;
			e.preventDefault();
			onClose();
			anchor?.focus();
		};
		document.addEventListener("mousedown", onDown);
		document.addEventListener("keydown", onKey);
		return () => {
			alive = false;
			teardown();
			document.removeEventListener("mousedown", onDown);
			document.removeEventListener("keydown", onKey);
		};
	});

	onDestroy(() => {
		if (closeTimer) clearTimeout(closeTimer);
		teardown();
	});

	// Portalled to the body: a menu left inside a sticky, backdrop-blurred
	// toolbar is clipped by it and stacks against it.
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() {
				node.parentNode?.removeChild(node);
			},
		};
	}
</script>

{#if shown}
	<div
		bind:this={menuEl}
		use:portal
		class="dd fixed z-[200] overflow-hidden rounded-md border border-border bg-bg-elevated shadow-3"
		class:dd-in={placed && !closing}
		class:dd-out={closing}
		class:dd-up={flipped}
		class:opacity-0={!placed}
		style:--dd-top="{menuTop}px"
		style:--dd-left="{menuLeft}px"
		style:--dd-width={matchAnchor ? `${menuWidth}px` : "auto"}
		style:--dd-min-width={minWidth ?? "0px"}
		style:--dd-out-ms="{OUT_MS}ms"
	>
		<ul
			bind:this={listEl}
			{role}
			aria-label={ariaLabel}
			class="overflow-y-auto py-1"
			style:max-height={maxH ? `${maxH}px` : undefined}
		>
			{@render children()}
		</ul>
	</div>
{/if}

<style>
	.dd {
		top: var(--dd-top);
		left: var(--dd-left);
		width: var(--dd-width);
		min-width: var(--dd-min-width);
	}
	/* Both directions are CSS animations rather than Svelte transitions, and the
	   unmount is on a timer — see the effect above for why. */
	.dd-in {
		animation: dd-in 140ms cubic-bezier(0.16, 1, 0.3, 1);
		transform-origin: top;
	}
	.dd-in.dd-up {
		animation-name: dd-in-up;
		transform-origin: bottom;
	}
	.dd-out {
		animation: dd-out var(--dd-out-ms) cubic-bezier(0.4, 0, 1, 1) forwards;
		transform-origin: top;
		/* A closing menu is not a target, and it must not swallow the press that
		   closed it or the one that opens the next menu. */
		pointer-events: none;
	}
	.dd-out.dd-up {
		animation-name: dd-out-up;
		transform-origin: bottom;
	}
	@keyframes dd-in {
		from {
			opacity: 0;
			transform: translateY(-4px) scaleY(0.97);
		}
	}
	@keyframes dd-in-up {
		from {
			opacity: 0;
			transform: translateY(4px) scaleY(0.97);
		}
	}
	@keyframes dd-out {
		to {
			opacity: 0;
			transform: translateY(-4px) scaleY(0.98);
		}
	}
	@keyframes dd-out-up {
		to {
			opacity: 0;
			transform: translateY(4px) scaleY(0.98);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.dd-in,
		.dd-out {
			animation: none;
		}
		.dd-out {
			opacity: 0;
		}
	}
</style>
