<script lang="ts">
	import type { Snippet } from "svelte";
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X } from "@lucide/svelte";
	import { twMerge, type ClassNameValue } from "tailwind-merge";
	import { closeOnBack } from "@lib/close-on-back.svelte";
	import { lockScroll, unlockScroll } from "@lib/scrollLock";
	import { sheetSwipe } from "@lib/sheet-swipe";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Mounted only while open: the caller's `{#if}` is what plays the transitions
	// and keeps its narrowing (`{#if item}`) valid inside the snippets.
	type Props = {
		onClose: () => void;
		label: string;
		title?: string;
		header?: Snippet;
		subheader?: Snippet;
		pinned?: Snippet;
		footer?: Snippet;
		children: Snippet;
		hideFrom?: "md" | "lg";
		class?: ClassNameValue;
		headerClass?: ClassNameValue;
		bodyClass?: ClassNameValue;
	};

	let {
		onClose,
		label,
		title,
		header,
		subheader,
		pinned,
		footer,
		children,
		hideFrom = "lg",
		class: className,
		headerClass,
		bodyClass,
	}: Props = $props();

	closeOnBack(
		() => true,
		() => onClose(),
	);

	$effect(() => {
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

<div
	class={twMerge("fixed inset-0 z-50", hideFrom === "md" ? "md:hidden" : "lg:hidden")}
	role="dialog"
	aria-modal="true"
	aria-label={label}
>
	<button
		type="button"
		aria-label={i18n.common_close()}
		data-sheet-scrim
		transition:fade={{ duration: 160 }}
		onclick={onClose}
		class="absolute inset-0 h-full w-full cursor-default bg-black/55"
	></button>

	<div
		use:sheetSwipe={{ onDismiss: onClose }}
		transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
		class={twMerge(
			"absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4",
			className,
		)}
	>
		<div
			class={twMerge(
				"relative flex cursor-grab touch-none select-none items-center gap-3 px-5 pb-2 pt-4 active:cursor-grabbing",
				headerClass,
			)}
		>
			<span
				aria-hidden="true"
				class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
			></span>
			{#if header}
				{@render header()}
			{:else}
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{title}</h2>
			{/if}
			<button
				type="button"
				onclick={onClose}
				aria-label={i18n.common_close()}
				class="ml-auto grid h-11 w-11 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
			>
				<X size={16} aria-hidden="true" />
			</button>
		</div>

		{@render subheader?.()}

		<div
			data-sheet-scroll
			class={twMerge("min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2", bodyClass)}
		>
			{@render children()}
		</div>

		{@render pinned?.()}

		{#if footer}
			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
				{@render footer()}
			</div>
		{/if}
	</div>
</div>
