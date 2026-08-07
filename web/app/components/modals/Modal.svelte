<script lang="ts">
	import type { Snippet } from "svelte";
	import { tick } from "svelte";
	import { fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X } from "@lucide/svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import {
		initialFocusTarget,
		portal,
		trapFocus,
	} from "../../lib/focus-trap";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		title: string;
		size?: "md" | "lg" | "xl" | "2xl" | "3xl" | "4xl";
		onClose: () => void;
		children: Snippet;
		footer?: Snippet;
	};

	let { open, title, size = "lg", onClose, children, footer }: Props = $props();

	let modalRoot = $state<HTMLDivElement | null>(null);
	let lastFocused: HTMLElement | null = null;
	let titleId = `modal-title-${Math.random().toString(36).slice(2, 10)}`;

	$effect(() => {
		if (!open) {
			if (lastFocused) {
				lastFocused.focus();
				lastFocused = null;
			}
			return;
		}
		lastFocused = document.activeElement as HTMLElement | null;
		lockScroll();
		tick().then(() => {
			if (!modalRoot) return;
			initialFocusTarget(modalRoot)?.focus();
		});
		return unlockScroll;
	});

	function onRootKeydown(e: KeyboardEvent) {
		if (e.key === "Escape") {
			e.stopPropagation();
			onClose();
		}
	}

	function onBackdropMousedown(e: MouseEvent) {
		// Only close on a click that started AND ended on the backdrop —
		// avoids closing when a drag-select begins inside the dialog and
		// releases on the backdrop.
		if (e.target === e.currentTarget) onClose();
	}

	// Re-home the overlay on <body> so the fixed backdrop is never clipped or
	// neutralised by a transformed / pointer-events-none ancestor (e.g. a card's
	// hover overlay). Lets a dialog be declared anywhere in the tree.
	function modalIn(_node: Element, params: { duration?: number } = {}) {
		const duration = params.duration ?? 180;
		return {
			duration,
			easing: cubicOut,
			css: (t: number) => `
				opacity: ${t};
				transform: translateY(${(1 - t) * 8}px) scale(${0.97 + 0.03 * t});
			`,
		};
	}

	const sizeClass = $derived(
		size === "md"
			? "max-w-md"
			: size === "lg"
				? "max-w-xl"
				: size === "xl"
					? "max-w-2xl"
					: size === "2xl"
						? "max-w-4xl"
						: size === "3xl"
							? "max-w-5xl"
							: "max-w-6xl",
	);
</script>

{#if open}
	<div
		use:portal
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-2 backdrop-blur-sm sm:px-4"
		transition:fade={{ duration: 180 }}
		onmousedown={onBackdropMousedown}
		role="presentation"
	>
		<div
			bind:this={modalRoot}
			use:trapFocus
			role="dialog"
			aria-modal="true"
			aria-labelledby={titleId}
			onkeydown={onRootKeydown}
			tabindex="-1"
			transition:modalIn|global
			class="w-full {sizeClass} overflow-hidden rounded-xl border border-border bg-bg-elevated text-fg shadow-3"
		>
			<div class="flex max-h-[85vh] flex-col">
				<header
					class="flex items-center justify-between border-b border-border px-5 py-3.5"
				>
					<h2
						id={titleId}
						class="text-base font-semibold tracking-tight text-fg"
					>
						{title}
					</h2>
					<button
						type="button"
						onclick={onClose}
						aria-label={i18n.common_close()}
						class="grid h-8 w-8 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						<X size={16} aria-hidden="true" />
					</button>
				</header>
				<div class="flex-1 overflow-y-auto px-5 py-4">
					{@render children()}
				</div>
				{#if footer}
					<footer
						class="flex items-center justify-end gap-2 border-t border-border px-5 py-3.5 [&_button]:flex-1 [&_button]:justify-center [&_button]:whitespace-nowrap sm:[&_button]:flex-none"
					>
						{@render footer()}
					</footer>
				{/if}
			</div>
		</div>
	</div>
{/if}
