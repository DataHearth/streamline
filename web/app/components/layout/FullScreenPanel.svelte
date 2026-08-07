<script lang="ts">
	import type { Snippet } from "svelte";
	import { fade } from "svelte/transition";
	import { ChevronLeft } from "@lucide/svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { initialFocusTarget, portal, trapFocus } from "../../lib/focus-trap";

	// A drilled-in screen on touch: back chevron, title, and the content at full
	// width. Used where a phone should get a page rather than a card in a box —
	// the account sections, which each hold a small form of their own.
	let {
		open,
		title,
		onClose,
		children,
	}: {
		open: boolean;
		title: string;
		onClose: () => void;
		children: Snippet;
	} = $props();

	let panel = $state<HTMLDivElement | null>(null);
	let lastFocused: HTMLElement | null = null;
	let titleId = `panel-title-${Math.random().toString(36).slice(2, 10)}`;

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
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				e.stopPropagation();
				onClose();
			}
		};
		document.addEventListener("keydown", onKey);
		requestAnimationFrame(() => {
			if (panel) initialFocusTarget(panel)?.focus();
		});
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

{#if open}
	<div use:portal class="fixed inset-0 z-50" transition:fade={{ duration: 120 }}>
		<div
			bind:this={panel}
			use:trapFocus
			role="dialog"
			aria-modal="true"
			aria-labelledby={titleId}
			class="flex h-full w-full flex-col bg-bg-deep"
		>
			<header
				class="flex h-14 shrink-0 items-center gap-1 border-b border-border bg-bg-elevated px-2"
			>
				<button
					type="button"
					onclick={onClose}
					class="grid h-11 w-11 shrink-0 place-items-center rounded-lg text-fg-muted transition active:bg-bg-hover"
					aria-label="Back"
				>
					<ChevronLeft size={20} aria-hidden="true" />
				</button>
				<h2
					id={titleId}
					class="min-w-0 flex-1 truncate pr-11 text-center text-[15px] font-semibold tracking-tight text-fg"
				>
					{title}
				</h2>
			</header>
			<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4">
				{@render children()}
				<div class="h-[max(env(safe-area-inset-bottom),16px)]"></div>
			</div>
		</div>
	</div>
{/if}
