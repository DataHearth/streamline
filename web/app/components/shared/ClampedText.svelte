<script lang="ts">
	import { tick } from "svelte";
	import { heroHold } from "@lib/hero-hold.svelte";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Text clamped to three lines, ending on "Read more" when the clamp hides
	// something. With `onReadMore` the caller owns the open state (`open`) and
	// there is no "Show less": the add sheet opens itself to full height.
	let {
		text,
		class: klass = "",
		textClass = "text-sm",
		open = false,
		onReadMore,
	}: {
		text: string;
		class?: string;
		textClass?: string;
		open?: boolean;
		onReadMore?: () => void;
	} = $props();

	// Collapsed again for each new text (client-side navigation). Through a
	// derived: the prop's getter also tracks the parent's object, which a
	// refetch replaces with the same text.
	let currentText = $derived(text);
	let ownExpanded = $derived.by(() => {
		void currentText;
		return false;
	});
	let expanded = $derived(onReadMore ? open : ownExpanded);
	let el = $state<HTMLParagraphElement | null>(null);
	let overflows = $state(false);

	$effect(() => {
		// Only measurable while collapsed; the last answer stands while expanded.
		void text;
		const node = el;
		if (!node) return;
		const measure = () => {
			if (!expanded) overflows = node.scrollHeight - node.clientHeight > 1;
		};
		measure();
		const ro = new ResizeObserver(measure);
		ro.observe(node);
		// A swapped-in web font reflows the text without resizing the box.
		document.fonts.addEventListener("loadingdone", measure);
		return () => {
			ro.disconnect();
			document.fonts.removeEventListener("loadingdone", measure);
		};
	});

	// A hero centres or bottom-aligns its text beside the art, so growing the
	// text would move the title up. Expanding pins that grid row's items where
	// they stand, and only what follows the text moves; collapsing unpins them.
	let unpin: (() => void) | null = null;
	function pinRow() {
		for (let a = el?.parentElement; a?.parentElement; a = a.parentElement) {
			const grid = a.parentElement;
			if (getComputedStyle(grid).display !== "grid") continue;
			const box = a.getBoundingClientRect();
			const row = [...grid.children].filter((c): c is HTMLElement => {
				const b = c.getBoundingClientRect();
				return c instanceof HTMLElement && b.bottom > box.top && b.top < box.bottom;
			});
			if (row.length < 2) return; // stacked: nothing sits beside the text
			const items = row.map((c) => ({ c, top: c.getBoundingClientRect().top }));
			const rowTop = Math.min(
				...items.map(({ c, top }) => top - parseFloat(getComputedStyle(c).marginTop)),
			);
			const saved = row.map((c) => [c, c.style.alignSelf, c.style.marginTop] as const);
			for (const { c, top } of items) {
				c.style.alignSelf = "start";
				c.style.marginTop = `${top - rowTop}px`;
			}
			const release = () => {
				for (const [c, align, margin] of saved) {
					c.style.alignSelf = align;
					c.style.marginTop = margin;
				}
				window.removeEventListener("resize", onResize);
				unpin = null;
			};
			// The offsets belong to this layout width, which a pinch-zoom or a
			// phone toolbar's `resize` leaves alone.
			const width = document.documentElement.clientWidth;
			const onResize = () => {
				if (document.documentElement.clientWidth !== width) release();
			};
			window.addEventListener("resize", onResize);
			unpin = release;
			return;
		}
	}

	$effect(() => {
		if (!expanded) unpin?.();
	});
	$effect(() => () => unpin?.());

	// Inside a hero, the backdrop keeps its height while this text is open.
	// A pre-effect runs before the text grows, so the hold measures it collapsed.
	const hold = heroHold();
	$effect.pre(() => {
		if (!expanded || !hold) return;
		hold.open();
		return () => hold.close();
	});

	async function toggle() {
		if (!onReadMore) {
			if (!ownExpanded) pinRow();
			ownExpanded = !ownExpanded;
			return;
		}
		// Opening removes the button; keep focus on the text, not <body>.
		const hadFocus = el?.contains(document.activeElement) ?? false;
		onReadMore();
		await tick();
		if (hadFocus) el?.focus({ preventScroll: true });
	}

	// The link is short of a thumb's target, so a tap on the text opens it
	// too, unless the click ends a text selection.
	function openFromText(e: MouseEvent) {
		if (expanded || !overflows) return;
		if (e.target instanceof Element && e.target.closest("button")) return;
		if (window.getSelection()?.toString()) return;
		toggle();
	}

	// Measured: the fade stops where the label starts, whatever the language.
	let toggleWidth = $state(0);
</script>

<!-- Collapsed, a float spacer (sized against the height the flex wrapper
     makes definite) fills every line but the last, so the toggle lands at the
     end of the last one. Expanded, the toggle follows the text instead; the
     text moves, not the button, so keyboard focus survives the switch.
     min-w-0: a long URL must not widen the paragraph past its column. -->
<div class={cn("flex max-w-[680px]", klass)}>
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions, a11y_no_noninteractive_tabindex -->
	<p
		bind:this={el}
		tabindex={onReadMore ? -1 : undefined}
		style:--toggle={toggleWidth ? `${toggleWidth}px` : undefined}
		onclick={openFromText}
		class={cn(
			"clamp min-w-0 break-words text-fg-muted outline-none [text-wrap:pretty]",
			textClass,
			!expanded && "clamped",
			!expanded && overflows && "faded cursor-pointer",
		)}
	>{#if expanded}{text}{/if}{#if overflows && !(onReadMore && expanded)}<button
				type="button"
				aria-expanded={expanded}
				bind:clientWidth={toggleWidth}
				onclick={toggle}
				class="toggle rounded font-medium text-accent-text underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
				>{expanded ? i18n.read_less() : i18n.read_more()}</button
			>{/if}{#if !expanded}{text}{/if}</p>
</div>

<style>
	.clamp {
		--lh: 1.625em;
		--toggle: 6em;
		line-height: var(--lh);
	}

	.clamped::before {
		content: "";
		float: right;
		height: calc(100% - var(--lh));
	}

	/* clip, not hidden: a hidden box still scrolls programmatically
	   (scrollIntoView, find-in-page) and opened the clamp mid-sentence. */
	.clamped {
		max-height: calc(var(--lh) * 3);
		overflow: clip;
	}

	/* The last line fades out before the link; the link stays whole. */
	.faded {
		--fade: linear-gradient(
			to right,
			#000 calc(100% - var(--toggle) - 4em),
			transparent calc(100% - var(--toggle) - 0.25em),
			transparent calc(100% - var(--toggle)),
			#000 calc(100% - var(--toggle))
		);
		mask-image: linear-gradient(#000, #000), var(--fade);
		mask-size:
			100% calc(100% - var(--lh)),
			100% var(--lh);
		mask-position: top, bottom;
		mask-repeat: no-repeat;
	}

	.toggle {
		float: right;
		clear: both;
		height: var(--lh);
		/* Part of the measured width: the label's first letter never fades. */
		padding-left: 0.5em;
	}
</style>
