<script lang="ts">
	import { cn } from "@lib/cn";
	import { LETTERS } from "@lib/music-books";

	// The phone's way through the letters, Android's fast scroller: the thumb
	// rides the list's position while it scrolls, and dragging it names the
	// letter under it in a bubble and takes the list there. A letter already
	// loaded is shown as the thumb passes it; one further on is fetched when the
	// thumb is let go, so a drag across the alphabet costs one page, not twenty.
	// The track is shared out by how many artists each letter holds, so the
	// thumb travels as far as the list does.
	let {
		letters,
		progress,
		active,
		onScrub,
	}: {
		letters: Record<string, number>;
		// Where the list is, 0 to 1.
		progress: number;
		// The list is scrolling: the thumb shows, and can be taken.
		active: boolean;
		// `final` is the letter the drag ended on.
		onScrub: (letter: string, final: boolean) => void;
	} = $props();

	let total = $derived(LETTERS.reduce((n, l) => n + (letters[l] ?? 0), 0));
	let track = $state<HTMLDivElement | null>(null);
	let dragging = $state(false);
	let at = $state(0);
	let letter = $state("");

	function letterAt(f: number): string {
		const target = f * total;
		let acc = 0;
		let last = "";
		for (const l of LETTERS) {
			const n = letters[l] ?? 0;
			if (!n) continue;
			last = l;
			if (target < acc + n) return l;
			acc += n;
		}
		return last;
	}
	function place(e: PointerEvent) {
		if (!track) return;
		const r = track.getBoundingClientRect();
		at = Math.min(1, Math.max(0, (e.clientY - r.top) / r.height));
		const l = letterAt(at);
		if (l && l !== letter) {
			letter = l;
			onScrub(l, false);
		}
	}
	function down(e: PointerEvent) {
		if (!track) return;
		e.preventDefault();
		track.setPointerCapture(e.pointerId);
		dragging = true;
		letter = "";
		place(e);
	}
	function move(e: PointerEvent) {
		if (dragging) place(e);
	}
	function up(e: PointerEvent) {
		if (!dragging) return;
		dragging = false;
		if (track?.hasPointerCapture(e.pointerId)) track.releasePointerCapture(e.pointerId);
		if (letter) onScrub(letter, true);
	}
	let pos = $derived((dragging ? at : progress) * 100);
</script>

<!-- Hidden from assistive tech: the list scrolls and filters without it. -->
<div
	bind:this={track}
	aria-hidden="true"
	onpointerdown={down}
	onpointermove={move}
	onpointerup={up}
	onpointercancel={up}
	class={cn(
		"fixed bottom-[calc(4.5rem+env(safe-area-inset-bottom))] right-0 top-[136px] z-30 w-6 touch-none select-none transition-opacity duration-200 md:hidden",
		active || dragging ? "opacity-100" : "pointer-events-none opacity-0",
	)}
>
	<div
		class={cn(
			"absolute bottom-0 right-1 top-0 w-1 rounded-full bg-white/[0.06] transition-opacity",
			dragging ? "opacity-100" : "opacity-0",
		)}
	></div>
	<div
		class={cn(
			"absolute right-[3px] h-11 w-1.5 -translate-y-1/2 rounded-full transition-colors",
			dragging ? "bg-accent" : "bg-fg-subtle",
		)}
		style:top="{pos}%"
	></div>
	{#if dragging && letter}
		<div
			class="pointer-events-none absolute right-7 grid h-[72px] w-[72px] -translate-y-full place-items-center rounded-[36px] rounded-br-[10px] border border-border-strong bg-bg-card shadow-4"
			style:top="{pos}%"
		>
			<div class="text-center leading-none">
				<div class="text-[30px] font-bold text-fg">{letter}</div>
				<div class="mt-1.5 font-mono text-[10.5px] text-fg-subtle">{(letters[letter] ?? 0).toLocaleString()}</div>
			</div>
		</div>
	{/if}
</div>
