<script lang="ts" generics="K extends string">
	import { tick } from "svelte";
	import { ChevronDown } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import MonitorToggle, { boxTone } from "@components/shared/MonitorToggle.svelte";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";

	// The hero bookmark with a chevron welded to it: the bookmark flips the
	// title's flag, the chevron opens one-shot choices (a series' presets).
	type Option = { key: K; label: string };
	type Props = {
		monitored: boolean;
		onToggle: (e: MouseEvent) => void;
		pending?: boolean;
		options: Option[];
		onPick: (key: K) => void;
		// The chevron's accessible name and the menu's heading.
		menuLabel: string;
		size?: "lg" | "md";
		backdrop?: boolean;
	};

	let {
		monitored,
		onToggle,
		pending = false,
		options,
		onPick,
		menuLabel,
		size = "lg",
		backdrop = false,
	}: Props = $props();

	let open = $state(false);
	let anchor = $state<HTMLDivElement | null>(null);
	let chevron = $state<HTMLButtonElement | null>(null);
	let items = $state<(HTMLButtonElement | null)[]>([]);

	function toggleMenu() {
		// aria-disabled, not disabled: a disabled control drops focus to <body>.
		if (pending && !open) return;
		open = !open;
		// The menu is portalled to <body>, so focus has to be moved into it.
		if (open) void tick().then(() => items[0]?.focus({ preventScroll: true }));
	}

	function onItemKey(e: KeyboardEvent, i: number) {
		const n = options.length;
		let next: number;
		if (e.key === "ArrowDown") next = (i + 1) % n;
		else if (e.key === "ArrowUp") next = (i - 1 + n) % n;
		else if (e.key === "Home") next = 0;
		else if (e.key === "End") next = n - 1;
		else if (e.key === "Tab") {
			e.preventDefault();
			open = false;
			chevron?.focus();
			return;
		} else return;
		e.preventDefault();
		items[next]?.focus();
	}

	function pick(key: K) {
		open = false;
		chevron?.focus();
		onPick(key);
	}
</script>

<!-- Anchored on the pair; the focused half is lifted over its neighbour. -->
<div bind:this={anchor} class="inline-flex shrink-0">
	<MonitorToggle
		{monitored}
		onToggle={(e) => {
			open = false;
			onToggle(e);
		}}
		{pending}
		{size}
		{backdrop}
		class="relative rounded-r-none focus-visible:z-10"
	/>
	<button
		bind:this={chevron}
		type="button"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-label={menuLabel}
		title={menuLabel}
		aria-disabled={pending || undefined}
		onclick={toggleMenu}
		class={cn(
			// 44px touch floor below lg.
			"relative grid w-11 shrink-0 place-items-center border border-l-0 transition focus:outline-none focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-accent-ring aria-disabled:cursor-not-allowed aria-disabled:opacity-50 lg:w-7",
			size === "md" ? "h-10 rounded-r-md" : "h-11 rounded-r-lg",
			boxTone(monitored, backdrop),
		)}
	>
		<ChevronDown
			size={14}
			class={cn("transition", open && "rotate-180")}
			aria-hidden="true"
		/>
	</button>
</div>

<DropdownMenu
	{open}
	{anchor}
	returnFocus={chevron}
	onClose={() => (open = false)}
	minWidth="200px"
	role="menu"
	ariaLabel={menuLabel}
>
	{#snippet header()}
		<p class="px-1.5 py-0.5 text-[11px] font-medium uppercase tracking-wide text-fg-subtle">
			{menuLabel}
		</p>
	{/snippet}
	{#each options as o, i (o.key)}
		<li role="none">
			<button
				bind:this={items[i]}
				type="button"
				role="menuitem"
				onclick={() => pick(o.key)}
				onkeydown={(e) => onItemKey(e, i)}
				class="flex min-h-11 w-full items-center px-3 py-1.5 text-left text-sm text-fg transition-colors hover:bg-bg-hover focus:outline-none focus-visible:bg-bg-hover lg:min-h-9"
			>
				{o.label}
			</button>
		</li>
	{/each}
</DropdownMenu>
