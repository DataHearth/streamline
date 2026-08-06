<script lang="ts">
	import { onMount, tick } from "svelte";
	import { Search, Film, Tv, ArrowRight } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { posterUrl, tvPosterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import {
		createSearchModel,
		itemKindLabel,
		searchNav,
		type SearchItem,
	} from "../../lib/search-model.svelte";

	let open = $state(false);
	let closing = $state(false);
	let query = $state("");
	let cursor = $state(0);
	let inputEl = $state<HTMLInputElement | null>(null);
	let dialogEl = $state<HTMLDialogElement | null>(null);
	let prevFocus: HTMLElement | null = null;

	// The list itself lives in lib/search-model: the phone screen and the tablet
	// panel show the same items, ordered for touch. Here it keeps the palette's
	// order — pages and actions first, titles under them.
	const model = createSearchModel(() => query, {});
	const navigateTo = searchNav();
	let sections = $derived(model.sections);
	let flat = $derived(model.flat);

	function indexOf(sectionIdx: number, itemIdx: number): number {
		const before = sections
			.slice(0, sectionIdx)
			.reduce((n, s) => n + s.items.length, 0);
		return before + itemIdx;
	}

	async function show() {
		if (open) return;
		prevFocus = document.activeElement as HTMLElement | null;
		open = true;
		query = "";
		cursor = 0;
		await tick();
		dialogEl?.showModal();
		await tick();
		inputEl?.focus();
	}

	function restoreFocus() {
		queueMicrotask(() => prevFocus?.focus());
	}

	function hide() {
		if (!open || closing) return;
		const d = dialogEl;
		if (!d?.open) {
			open = false;
			return;
		}
		// Reduced motion: close immediately, no exit transition to wait on.
		if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
			open = false;
			d.close();
			restoreFocus();
			return;
		}
		// Animate the exit by fading the box out (.is-closing) while the dialog
		// stays open, then close() on transitionend. close()-ing first and
		// relying on a display/overlay allow-discrete transition doesn't work
		// here — that discrete transition gets cancelled mid-flight.
		let timer = 0;
		const finish = () => {
			window.clearTimeout(timer);
			d.removeEventListener("transitionend", onEnd);
			closing = false;
			open = false;
			d.close();
			restoreFocus();
		};
		const onEnd = (e: TransitionEvent) => {
			if (e.target === d && e.propertyName === "opacity") finish();
		};
		closing = true;
		d.addEventListener("transitionend", onEnd);
		timer = window.setTimeout(finish, 220);
	}

	function onCancel(e: Event) {
		// Native Escape closes instantly with no exit animation — run ours.
		e.preventDefault();
		hide();
	}

	function activate(item: SearchItem) {
		hide();
		navigateTo(item);
	}

	function onKeydown(e: KeyboardEvent) {
		if (!open) {
			if ((e.key === "k" || e.key === "K") && (e.metaKey || e.ctrlKey)) {
				const active = document.activeElement as HTMLElement | null;
				const isInField =
					active &&
					(active.tagName === "INPUT" ||
						active.tagName === "TEXTAREA" ||
						active.isContentEditable);
				if (isInField) return;
				e.preventDefault();
				show();
			}
			return;
		}
		// Escape is left to the native <dialog>, which closes with the exit
		// animation and fires `close` → onclose={hide} for state cleanup.
		if (e.key === "ArrowDown") {
			e.preventDefault();
			if (flat.length === 0) return;
			cursor = (cursor + 1) % flat.length;
			scrollCursorIntoView();
		} else if (e.key === "ArrowUp") {
			e.preventDefault();
			if (flat.length === 0) return;
			cursor = (cursor - 1 + flat.length) % flat.length;
			scrollCursorIntoView();
		} else if (e.key === "Enter") {
			e.preventDefault();
			const item = flat[cursor];
			if (item) activate(item);
		}
	}

	function scrollCursorIntoView() {
		queueMicrotask(() => {
			dialogEl
				?.querySelector<HTMLElement>("[data-cmd-active]")
				?.scrollIntoView({ block: "nearest" });
		});
	}

	function onOpenEvent() {
		show();
	}

	function onBackdropClick(e: MouseEvent) {
		if (e.target === dialogEl) hide();
	}

	$effect(() => {
		// Reset cursor when query or sections shrink
		void query;
		void sections;
		cursor = 0;
	});

	onMount(() => {
		window.addEventListener("keydown", onKeydown);
		window.addEventListener("streamline:open-palette", onOpenEvent);
		return () => {
			window.removeEventListener("keydown", onKeydown);
			window.removeEventListener("streamline:open-palette", onOpenEvent);
		};
	});
</script>

<dialog
	bind:this={dialogEl}
	aria-label={i18n.palette_label()}
	class="palette max-h-[70dvh] w-[min(640px,92vw)] overflow-hidden rounded-xl border border-border-strong bg-bg-elevated text-fg shadow-4"
	class:is-closing={closing}
	onclick={onBackdropClick}
	oncancel={onCancel}
	onclose={hide}
>
	<div class="flex flex-col max-h-[70dvh]">
			<div
				class="search-row flex items-center gap-3 border-b border-border px-4 py-3"
			>
				<Search
					size={16}
					class="search-icon shrink-0 text-fg-subtle"
					aria-hidden="true"
				/>
				<input
					bind:this={inputEl}
					bind:value={query}
					type="text"
					placeholder={i18n.palette_placeholder()}
					class="search-input flex-1 border-0 bg-transparent text-[15px] text-fg placeholder:text-fg-faint"
					autocomplete="off"
					spellcheck="false"
				/>
				<kbd
					class="rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-[10px] text-fg-faint"
					>ESC</kbd
				>
			</div>

			<div class="flex-1 overflow-y-auto p-1.5">
				{#each sections as section, sIdx (section.label)}
					<div
						class="px-3 pt-2.5 pb-1 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
					>
						{section.label}
					</div>
					{#each section.items as item, iIdx (item.kind + "-" + iIdx)}
						{@const flatIdx = indexOf(sIdx, iIdx)}
						{@const active = flatIdx === cursor}
						<button
							type="button"
							onmouseenter={() => (cursor = flatIdx)}
							onclick={() => activate(item)}
							data-cmd-active={active ? "" : undefined}
							class={cn(
								"flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors",
								active
									? "bg-accent-soft text-fg"
									: "text-fg-muted hover:text-fg",
							)}
						>
							{#if item.kind === "movie" || item.kind === "series"}
								{@const MediaIcon = item.kind === "movie" ? Film : Tv}
								{@const poster =
									item.kind === "movie"
										? posterUrl({ id: item.id })
										: tvPosterUrl(item.id)}
								<div
									class="relative h-9 w-6 shrink-0 overflow-hidden rounded-md bg-surface-2"
								>
									<div
										class="absolute inset-0 grid place-items-center text-fg-muted"
									>
										<MediaIcon size={14} aria-hidden="true" />
									</div>
									<Poster
										src={poster}
										alt="{item.label} poster"
										class="relative h-full w-full object-cover"
									/>
								</div>
							{:else}
								{@const Icon = item.icon}
								<div
									class={cn(
										"grid h-7 w-7 shrink-0 place-items-center rounded-md transition-colors",
										active
											? "bg-accent text-fg-on-accent"
											: "bg-surface-2 text-fg-muted",
									)}
								>
									<Icon size={14} aria-hidden="true" />
								</div>
							{/if}
							<div class="min-w-0 flex-1">
								<div class="truncate text-[13px] font-medium">
									{item.label}
								</div>
								{#if (item.kind === "movie" || item.kind === "series") && item.year}
									<div
										class="truncate font-mono text-[10.5px] text-fg-subtle"
									>
										{item.year}
									</div>
								{/if}
							</div>
							<span
								class="font-mono text-[9.5px] uppercase tracking-[0.1em] text-fg-faint"
							>
								{itemKindLabel(item)}
							</span>
							{#if active}
								<ArrowRight
									size={12}
									class="shrink-0 text-fg-faint"
									aria-hidden="true"
								/>
							{/if}
						</button>
					{/each}
				{/each}

				{#if flat.length === 0}
					<div class="px-3 py-8 text-center text-[12.5px] text-fg-subtle">
						No matches for "{query}"
					</div>
				{/if}
			</div>

			<footer
				class="flex items-center gap-4 border-t border-border bg-bg-base px-4 py-2 font-mono text-[10.5px] text-fg-faint"
			>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↑</kbd
					><kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↓</kbd
					> navigate
				</span>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↵</kbd
					> select
				</span>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>{i18n.palette_esc()}</kbd
					> close
				</span>
			</footer>
		</div>
</dialog>

<style>
	/* Open/close animation matches Modal.svelte (modalIn): opacity + 8px
	   translateY + 0.97→1 scale, with a faster exit.

	   Enter rides the native showModal() via @starting-style. The exit is
	   driven by JS (hide()): it adds .is-closing to fade the box *while the
	   dialog stays open*, then calls close() on transitionend. We deliberately
	   avoid display/overlay `allow-discrete` — that discrete transition starts
	   late and gets cancelled mid-flight in this app, killing the exit. */
	.palette {
		margin: 12vh auto 0;
		opacity: 0;
		transform: translateY(8px) scale(0.97);
	}
	.palette[open] {
		opacity: 1;
		transform: none;
		/* enter timing matches Modal's 180ms cubicOut */
		transition:
			opacity 180ms cubic-bezier(0.33, 1, 0.68, 1),
			transform 180ms cubic-bezier(0.33, 1, 0.68, 1);
	}
	@starting-style {
		.palette[open] {
			opacity: 0;
			transform: translateY(8px) scale(0.97);
		}
	}
	/* exit: faster (~67% of enter), ease-in */
	.palette[open].is-closing {
		opacity: 0;
		transform: translateY(8px) scale(0.97);
		transition:
			opacity 120ms cubic-bezier(0.32, 0, 0.67, 0),
			transform 120ms cubic-bezier(0.32, 0, 0.67, 0);
	}
	.palette::backdrop {
		background: rgb(2 2 3 / 0);
		backdrop-filter: blur(0);
	}
	.palette[open]::backdrop {
		background: rgb(2 2 3 / 0.6);
		backdrop-filter: blur(8px);
		transition:
			background 180ms ease-out,
			backdrop-filter 180ms ease-out;
	}
	@starting-style {
		.palette[open]::backdrop {
			background: rgb(2 2 3 / 0);
			backdrop-filter: blur(0);
		}
	}
	.palette[open].is-closing::backdrop {
		background: rgb(2 2 3 / 0);
		backdrop-filter: blur(0);
		transition:
			background 120ms ease-in,
			backdrop-filter 120ms ease-in;
	}
	@media (prefers-reduced-motion: reduce) {
		.palette[open],
		.palette[open].is-closing,
		.palette[open]::backdrop,
		.palette[open].is-closing::backdrop {
			transition: none;
		}
	}
	.search-row:focus-within {
		background: color-mix(in srgb, var(--accent) 4%, transparent);
	}
	.search-row:focus-within :global(.search-icon) {
		color: var(--accent);
	}
	.search-input:focus,
	.search-input:focus-visible {
		outline: none;
		box-shadow: none;
		border-radius: 0;
	}
</style>
