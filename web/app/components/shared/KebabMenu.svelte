<script lang="ts" module>
	import type { LucideIcon } from "@lucide/svelte";

	export type KebabItem = {
		key: string;
		label: string;
		icon: LucideIcon;
		danger?: boolean;
		disabled?: boolean;
		title?: string;
		// Draws a divider above this item — the destructive group is fenced off
		// from the routine actions so a mis-aimed click can't delete a library
		// entry.
		dividerBefore?: boolean;
		onSelect: () => void;
	};
</script>

<script lang="ts">
	import { onDestroy, tick } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Ellipsis, EllipsisVertical } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		items,
		variant = "toolbar",
	}: {
		items: KebabItem[];
		// "bar" sits inside the bulk-action bar, which already supplies the
		// elevated surface — so it stays borderless and matches the sibling pills.
		variant?: "toolbar" | "card" | "bar";
	} = $props();

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);
	let menuEl = $state<HTMLDivElement | null>(null);
	let menuTop = $state(0);
	let menuRight = $state(0);

	const MENU_W = 240;
	const MENU_GAP = 6;

	function recompute() {
		if (!triggerEl) return;
		const rect = triggerEl.getBoundingClientRect();
		// Close if trigger leaves the viewport entirely.
		if (
			rect.bottom < 0 ||
			rect.top > window.innerHeight ||
			rect.right < 0 ||
			rect.left > window.innerWidth
		) {
			close();
			return;
		}
		// Right-aligned to the trigger, but clamped to both edges: a left-column
		// card on a phone sits far enough from the right that a right-anchored
		// 240px menu would start off the left of the screen.
		const maxRight = Math.max(8, window.innerWidth - MENU_W - 8);
		menuRight = Math.min(maxRight, Math.max(8, window.innerWidth - rect.right));
		// Open downward by default; flip above the trigger when there's not
		// enough room below but enough above (bottom-row cards in a grid).
		const below = rect.bottom + MENU_GAP;
		const h = menuEl?.offsetHeight ?? 0;
		if (h && window.innerHeight - below < h && rect.top - MENU_GAP > h) {
			menuTop = rect.top - MENU_GAP - h;
		} else {
			menuTop = below;
		}
	}

	async function openMenu() {
		open = true;
		await tick();
		recompute();
		window.addEventListener("scroll", recompute, true);
		window.addEventListener("resize", recompute);
	}

	function close() {
		if (!open) return;
		open = false;
		window.removeEventListener("scroll", recompute, true);
		window.removeEventListener("resize", recompute);
		triggerEl?.focus();
	}

	function toggle() {
		if (open) close();
		else openMenu();
	}

	function pick(item: KebabItem) {
		close();
		item.onSelect();
	}

	function onKey(e: KeyboardEvent) {
		if (!open) return;
		if (e.key === "Escape") {
			e.preventDefault();
			close();
		}
	}

	function onDocClick(e: MouseEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (menuEl?.contains(t)) return;
		if (triggerEl?.contains(t)) return;
		close();
	}

	$effect(() => {
		if (open) {
			document.addEventListener("mousedown", onDocClick);
			document.addEventListener("keydown", onKey);
			return () => {
				document.removeEventListener("mousedown", onDocClick);
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

<button
	bind:this={triggerEl}
	type="button"
	aria-haspopup="menu"
	aria-expanded={open}
	aria-label={i18n.common_more_actions()}
	onclick={toggle}
	class={cn(
		"grid place-items-center transition focus:outline-none focus:ring-2 focus:ring-accent-ring",
		variant === "card"
			? "h-7 w-7 rounded-full border border-white/10 bg-black/65 text-white backdrop-blur-sm hover:bg-black/80"
			: variant === "bar"
				? "h-9 w-9 rounded-md border border-border bg-bg-elevated text-fg-muted hover:border-border-strong hover:text-fg"
				: "h-10 w-10 rounded-md border border-border-strong bg-white/[0.08] text-fg backdrop-blur-sm hover:bg-white/[0.14]",
	)}
>
	{#if variant === "card"}
		<EllipsisVertical class="h-3.5 w-3.5" aria-hidden="true" />
	{:else}
		<Ellipsis class="h-4 w-4" aria-hidden="true" />
	{/if}
</button>

{#if open}
	<div
		bind:this={menuEl}
		use:portal
		role="menu"
		transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
		class="kebab-menu fixed z-50 overflow-hidden rounded-md border border-border-strong bg-bg-elevated shadow-4"
		style:--menu-top="{menuTop}px"
		style:--menu-right="{menuRight}px"
		style:--menu-width="{MENU_W}px"
	>
		{#each items as item (item.key)}
			{@const Icon = item.icon}
			{#if item.dividerBefore}
				<div class="h-px bg-border" role="separator"></div>
			{/if}
			<button
				role="menuitem"
				type="button"
				disabled={item.disabled}
				title={item.title}
				onclick={() => pick(item)}
				class={cn(
					"flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent",
					item.danger
						? "text-status-failed hover:bg-status-failed/10"
						: "hover:bg-bg-hover",
				)}
			>
				<Icon
					class={cn("h-4 w-4", !item.danger && "text-fg-muted")}
					aria-hidden="true"
				/>
				{item.label}
			</button>
		{/each}
	</div>
{/if}

<style>
	.kebab-menu {
		top: var(--menu-top);
		right: var(--menu-right);
		width: min(var(--menu-width), calc(100vw - 16px));
	}
</style>
