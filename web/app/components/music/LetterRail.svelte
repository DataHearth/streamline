<script lang="ts">
	import { onMount } from "svelte";
	import { cn } from "@lib/cn";
	import { LETTERS } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The letters down the right edge from md, the iOS index: a click jumps to
	// one. On a touch screen a letter is too small to aim for, so a drag along
	// the rail picks the nearest one under the finger and shows it in the phone
	// scroller's bubble. A letter the filters leave empty is dimmed and skipped.
	// The rail centres in the space under the top bar and its letters shrink
	// when that space is shorter than they are.
	let {
		letters,
		current,
		onScrub,
	}: {
		letters: Record<string, number>;
		// The letter at the top of the list.
		current: string | null;
		// `final` is the letter a click or a drag ended on.
		onScrub: (letter: string, final: boolean) => void;
	} = $props();

	// #main keeps a stable scrollbar gutter from lg (AppShell); the rail stands
	// just inside it, never over the bar.
	let gutter = $state(0);
	onMount(() => {
		const main = document.getElementById("main");
		if (!main) return;
		const measure = () => (gutter = main.offsetWidth - main.clientWidth);
		measure();
		const ro = new ResizeObserver(measure);
		ro.observe(main);
		return () => ro.disconnect();
	});

	let nav = $state<HTMLElement | null>(null);
	let dragging = $state(false);
	// The letter under the finger and its centre, from the rail's top.
	let held = $state<{ letter: string; y: number } | null>(null);

	function nearest(y: number) {
		if (!nav) return null;
		const top = nav.getBoundingClientRect().top;
		let best: { letter: string; y: number } | null = null;
		let gap = Infinity;
		for (const b of nav.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")) {
			const r = b.getBoundingClientRect();
			const mid = r.top + r.height / 2;
			if (Math.abs(mid - y) < gap) {
				gap = Math.abs(mid - y);
				best = { letter: b.dataset.letter ?? "", y: mid - top };
			}
		}
		return best;
	}
	function down(e: PointerEvent) {
		// A mouse clicks the letters themselves.
		if (e.pointerType === "mouse" || !nav) return;
		e.preventDefault();
		nav.setPointerCapture(e.pointerId);
		dragging = true;
		held = null;
		move(e);
	}
	function move(e: PointerEvent) {
		if (!dragging) return;
		const at = nearest(e.clientY);
		if (!at) return;
		if (at.letter !== held?.letter) onScrub(at.letter, false);
		held = at;
	}
	function up(e: PointerEvent) {
		if (!dragging) return;
		dragging = false;
		if (nav?.hasPointerCapture(e.pointerId)) nav.releasePointerCapture(e.pointerId);
		if (held) onScrub(held.letter, true);
		held = null;
	}
</script>

<nav
	bind:this={nav}
	aria-label={i18n.music_jump_letters()}
	onpointerdown={down}
	onpointermove={move}
	onpointerup={up}
	onpointercancel={up}
	style:right="{gutter}px"
	class="fixed top-[calc(50%+var(--topbar-h)/2)] z-20 hidden max-h-[calc(100%-var(--topbar-h)-2rem)] w-6 -translate-y-1/2 touch-none select-none flex-col items-center md:flex"
>
	{#each LETTERS as l (l)}
		{@const has = (letters[l] ?? 0) > 0}
		<button
			type="button"
			data-letter={l}
			disabled={!has}
			onclick={() => onScrub(l, true)}
			aria-label={i18n.music_jump_to({ letter: l })}
			aria-current={l === current ? "true" : undefined}
			class={cn(
				"grid h-[22px] min-h-0 w-6 shrink place-items-center rounded-md font-mono text-[10.5px] leading-none transition",
				l === current
					? "bg-accent-soft font-semibold text-accent-text"
					: has
						? "text-fg-muted hover:bg-surface hover:text-fg"
						: "cursor-default text-fg-faint opacity-50",
			)}
		>
			{l}
		</button>
	{/each}
	{#if dragging && held}
		<div
			class="pointer-events-none absolute right-full mr-2 grid h-[72px] w-[72px] -translate-y-1/2 place-items-center rounded-[36px] rounded-r-[10px] border border-border-strong bg-bg-card shadow-4"
			style:top="{held.y}px"
			aria-hidden="true"
		>
			<div class="text-center leading-none">
				<div class="text-[30px] font-bold text-fg">{held.letter}</div>
				<div class="mt-1.5 font-mono text-[10.5px] text-fg-subtle">{(letters[held.letter] ?? 0).toLocaleString()}</div>
			</div>
		</div>
	{/if}
</nav>
