<script lang="ts" module>
	export type SeriesAction =
		| "search"
		| "quality"
		| "rename"
		| "refresh"
		| "delete-files"
		| "delete"
		| "delete-with-files";
</script>

<script lang="ts">
	import { onDestroy, tick } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import {
		MoreHorizontal,
		MoreVertical,
		Search,
		Gauge,
		FileEdit,
		RefreshCw,
		Trash2,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";

	let {
		onPick,
		disabledActions = [],
		variant = "toolbar",
		allowDeleteFiles = false,
	}: {
		onPick: (a: SeriesAction) => void;
		disabledActions?: SeriesAction[];
		variant?: "toolbar" | "card";
		// "Delete all files" (keep the series, revert to wanted) needs the loaded
		// episode list, so it's only offered from the detail hero, not grid cards.
		allowDeleteFiles?: boolean;
	} = $props();

	const isDisabled = (a: SeriesAction) => disabledActions.includes(a);

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
		if (
			rect.bottom < 0 ||
			rect.top > window.innerHeight ||
			rect.right < 0 ||
			rect.left > window.innerWidth
		) {
			close();
			return;
		}
		menuRight = Math.max(8, window.innerWidth - rect.right);
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

	function pick(a: SeriesAction) {
		close();
		onPick(a);
	}

	function onKey(e: KeyboardEvent) {
		if (open && e.key === "Escape") {
			e.preventDefault();
			close();
		}
	}

	function onDocClick(e: MouseEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (menuEl?.contains(t) || triggerEl?.contains(t)) return;
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
	aria-label="More actions"
	onclick={toggle}
	class={cn(
		"grid place-items-center transition focus:outline-none focus:ring-2 focus:ring-accent-ring",
		variant === "card"
			? "h-7 w-7 rounded-full border border-white/10 bg-black/65 text-white backdrop-blur-sm hover:bg-black/80"
			: "h-10 w-10 rounded-md border border-border-strong bg-white/[0.08] text-fg backdrop-blur-sm hover:bg-white/[0.14]",
	)}
>
	{#if variant === "card"}
		<MoreVertical class="h-3.5 w-3.5" aria-hidden="true" />
	{:else}
		<MoreHorizontal class="h-4 w-4" aria-hidden="true" />
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
		<button
			role="menuitem"
			type="button"
			onclick={() => pick("search")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition hover:bg-bg-hover"
		>
			<Search class="h-4 w-4 text-fg-muted" aria-hidden="true" />
			Search for wanted episodes
		</button>
		<button
			role="menuitem"
			type="button"
			onclick={() => pick("quality")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition hover:bg-bg-hover"
		>
			<Gauge class="h-4 w-4 text-fg-muted" aria-hidden="true" />
			Change quality profile…
		</button>
		<button
			role="menuitem"
			type="button"
			disabled={isDisabled("rename")}
			title={isDisabled("rename")
				? "Available once episodes have been imported"
				: undefined}
			onclick={() => pick("rename")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition hover:bg-bg-hover disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
		>
			<FileEdit class="h-4 w-4 text-fg-muted" aria-hidden="true" />
			Rename files…
		</button>
		<button
			role="menuitem"
			type="button"
			onclick={() => pick("refresh")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition hover:bg-bg-hover"
		>
			<RefreshCw class="h-4 w-4 text-fg-muted" aria-hidden="true" />
			Refresh metadata
		</button>
		<div class="h-px bg-border" role="separator"></div>
		{#if allowDeleteFiles}
			<button
				role="menuitem"
				type="button"
				disabled={isDisabled("delete-files")}
				onclick={() => pick("delete-files")}
				class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-status-failed transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
			>
				<Trash2 class="h-4 w-4" aria-hidden="true" />
				Delete all files
			</button>
		{/if}
		<button
			role="menuitem"
			type="button"
			onclick={() => pick("delete")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-status-failed transition hover:bg-status-failed/10"
		>
			<Trash2 class="h-4 w-4" aria-hidden="true" />
			Delete from library
		</button>
		<button
			role="menuitem"
			type="button"
			disabled={isDisabled("delete-with-files")}
			onclick={() => pick("delete-with-files")}
			class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-status-failed transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
		>
			<Trash2 class="h-4 w-4" aria-hidden="true" />
			Delete + files
		</button>
	</div>
{/if}

<style>
	.kebab-menu {
		top: var(--menu-top);
		right: var(--menu-right);
		width: var(--menu-width);
	}
</style>
