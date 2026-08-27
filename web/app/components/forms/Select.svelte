<script lang="ts" generics="T extends string">
	import { onDestroy, tick } from "svelte";
	import { ChevronDown, Check } from "@lucide/svelte";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";
	import { readOnlyLock } from "../../lib/config.svelte";
	import FieldLock from "./FieldLock.svelte";

	// Expand/collapse from the trigger edge: fade + slide + subtle scaleY off
	// transform-origin top. Honors prefers-reduced-motion.
	function dropdown(_node: HTMLElement) {
		const reduce = window.matchMedia(
			"(prefers-reduced-motion: reduce)",
		).matches;
		const dir = flipped ? -1 : 1;
		return {
			duration: reduce ? 0 : 170,
			easing: cubicOut,
			css: (t: number) =>
				`opacity:${t};transform-origin:${flipped ? "bottom" : "top"};transform:translateY(${(t - 1) * 8 * dir}px) scaleY(${0.95 + t * 0.05})`,
		};
	}

	// hint renders as a muted second line under the option's label in the
	// dropdown — used e.g. to surface a custom format's description. It is
	// never shown in the closed (selected-value) trigger, which stays
	// label-only.
	type Option = { value: T; label: string; hint?: string };

	type Props = {
		label?: string;
		value: T;
		options: Option[];
		onChange: (v: T) => void;
		id?: string;
		disabled?: boolean;
		// Accessible name for the label-less case (e.g. a compact toolbar filter
		// where the selected value already communicates the control's purpose).
		ariaLabel?: string;
		// Opt out of the read-only lock, for a picker whose value the operator
		// needs to choose even though this instance cannot save it — the Plex
		// section pickers, whose whole purpose on a read-only instance is to let
		// someone identify which library is theirs and copy its key into config.
		// See readOnlyLock(), which already names discovery as exempt.
		readOnlyExempt?: boolean;
	};

	let {
		label,
		value,
		options,
		onChange,
		id,
		disabled = false,
		ariaLabel,
		readOnlyExempt = false,
	}: Props = $props();

	const lock = readOnlyLock();
	let configLocked = $derived(!readOnlyExempt && lock());
	let off = $derived(disabled || configLocked);

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);
	let menuEl = $state<HTMLDivElement | null>(null);
	let menuTop = $state(0);
	let menuLeft = $state(0);
	let menuWidth = $state(0);
	let flipped = $state(false);
	let menuObserver: ResizeObserver | null = null;
	const MENU_GAP = 8;
	const VIEWPORT_PAD = 8;

	let selectedLabel = $derived(
		options.find((o) => o.value === value)?.label ?? "",
	);

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
		menuWidth = r.width;
		// Keep the menu inside the viewport horizontally (triggers near the right
		// edge would otherwise push it off-screen).
		menuLeft = Math.max(
			VIEWPORT_PAD,
			Math.min(r.left, window.innerWidth - r.width - VIEWPORT_PAD),
		);
		// Flip above the trigger when the menu wouldn't fit below it — the case
		// for any select sitting in a modal footer or near the viewport bottom.
		const menuH = menuEl?.offsetHeight ?? 0;
		const below = window.innerHeight - r.bottom - MENU_GAP - VIEWPORT_PAD;
		const above = r.top - MENU_GAP - VIEWPORT_PAD;
		flipped = menuH > below && above > below;
		menuTop = flipped
			? Math.max(VIEWPORT_PAD, r.top - MENU_GAP - menuH)
			: r.bottom + MENU_GAP;
	}

	async function openMenu() {
		if (off) return;
		open = true;
		await tick();
		recompute();
		// The portalled menu isn't at its final height one tick after mount (the
		// browser Tailwind JIT hasn't emitted its utilities yet), and the flip-up
		// branch positions from that height. Re-measure whenever it settles — this
		// also covers webfont load and option-list changes.
		if (menuEl && typeof ResizeObserver !== "undefined") {
			menuObserver = new ResizeObserver(() => recompute());
			menuObserver.observe(menuEl);
		}
		window.addEventListener("scroll", recompute, true);
		window.addEventListener("resize", recompute);
	}

	function close() {
		if (!open) return;
		open = false;
		menuObserver?.disconnect();
		menuObserver = null;
		window.removeEventListener("scroll", recompute, true);
		window.removeEventListener("resize", recompute);
	}

	function toggle() {
		if (open) close();
		else openMenu();
	}

	function pick(v: T) {
		onChange(v);
		close();
		triggerEl?.focus();
	}

	function onKey(e: KeyboardEvent) {
		if (!open) return;
		if (e.key === "Escape") {
			e.preventDefault();
			close();
			triggerEl?.focus();
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
		menuObserver?.disconnect();
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

<div class="block">
	{#if label}
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
			>{label}<FieldLock locked={configLocked} /></span
		>
	{/if}
	<div class="relative">
		<button
			bind:this={triggerEl}
			{id}
			type="button"
			disabled={off}
			aria-label={label ? undefined : ariaLabel}
			aria-haspopup="listbox"
			aria-expanded={open}
			onclick={toggle}
			class={cn(
				"flex h-[38px] w-full items-center justify-between gap-2 rounded-md border border-border bg-bg px-3 text-sm text-fg transition-colors hover:border-border-strong focus-visible:outline-2 focus-visible:outline-accent",
				open && "border-accent",
				off && "cursor-not-allowed opacity-60",
			)}
		>
			<span class="truncate">{selectedLabel}</span>
			<ChevronDown
				size={16}
				class={cn(
					"shrink-0 text-fg-muted transition-transform duration-150",
					open && "rotate-180",
				)}
				aria-hidden="true"
			/>
		</button>
	</div>
</div>

{#if open}
	<div
		bind:this={menuEl}
		use:portal
		transition:dropdown
		class="select-menu fixed z-[200] overflow-hidden rounded-md border border-border bg-bg-elevated shadow-3"
		style:--menu-top="{menuTop}px"
		style:--menu-left="{menuLeft}px"
		style:--menu-width="{menuWidth}px"
	>
		<ul role="listbox" class="max-h-60 overflow-y-auto py-1">
			{#each options as o (o.value)}
				<li>
					<button
						type="button"
						role="option"
						aria-selected={value === o.value}
						title={o.hint}
						onclick={() => pick(o.value)}
						class={cn(
							"flex w-full items-start justify-between gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-bg-hover",
							value === o.value && "text-accent",
						)}
					>
						<span class="min-w-0 flex-1">
							<span class="block truncate">{o.label}</span>
							{#if o.hint}
								<span
									class="mt-0.5 line-clamp-2 block text-xs font-normal text-fg-subtle"
								>
									{o.hint}
								</span>
							{/if}
						</span>
						{#if value === o.value}
							<Check
								size={14}
								class="mt-0.5 shrink-0 text-accent"
								aria-hidden="true"
							/>
						{/if}
					</button>
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
	.select-menu {
		top: var(--menu-top);
		left: var(--menu-left);
		width: var(--menu-width);
	}
</style>
