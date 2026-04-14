<script lang="ts" generics="T extends string">
	import { onDestroy, tick } from "svelte";
	import { ChevronDown, Check } from "@lucide/svelte";
	import { cubicOut } from "svelte/easing";
	import { cn } from "../../lib/cn";

	// Expand/collapse from the trigger edge: fade + slide + subtle scaleY off
	// transform-origin top. Honors prefers-reduced-motion.
	function dropdown(_node: HTMLElement) {
		const reduce = window.matchMedia(
			"(prefers-reduced-motion: reduce)",
		).matches;
		return {
			duration: reduce ? 0 : 170,
			easing: cubicOut,
			css: (t: number) =>
				`opacity:${t};transform-origin:top;transform:translateY(${(t - 1) * 8}px) scaleY(${0.95 + t * 0.05})`,
		};
	}

	type Option = { value: T; label: string };

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
	};

	let {
		label,
		value,
		options,
		onChange,
		id,
		disabled = false,
		ariaLabel,
	}: Props = $props();

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);
	let menuEl = $state<HTMLDivElement | null>(null);
	let menuTop = $state(0);
	let menuLeft = $state(0);
	let menuWidth = $state(0);
	const MENU_GAP = 8;

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
		menuTop = r.bottom + MENU_GAP;
		menuLeft = r.left;
		menuWidth = r.width;
	}

	async function openMenu() {
		if (disabled) return;
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
		<span class="mb-1 block text-sm font-medium text-fg">{label}</span>
	{/if}
	<div class="relative">
		<button
			bind:this={triggerEl}
			{id}
			type="button"
			{disabled}
			aria-label={label ? undefined : ariaLabel}
			aria-haspopup="listbox"
			aria-expanded={open}
			onclick={toggle}
			class={cn(
				"flex h-[38px] w-full items-center justify-between gap-2 rounded-md border border-border bg-bg px-3 text-sm text-fg transition-colors hover:border-border-strong focus-visible:outline-2 focus-visible:outline-accent",
				open && "border-accent",
				disabled && "cursor-not-allowed opacity-60",
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
						onclick={() => pick(o.value)}
						class={cn(
							"flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-bg-hover",
							value === o.value && "text-accent",
						)}
					>
						<span class="min-w-0 flex-1">{o.label}</span>
						{#if value === o.value}
							<Check
								size={14}
								class="text-accent"
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
