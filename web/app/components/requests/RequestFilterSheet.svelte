<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import {
		KIND_CHIPS,
		statusChips,
		type RequestKind,
		type RequestTab,
	} from "../../lib/requests-touch";
	import type { RequestCounts } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Everything the line above can no longer afford to keep on screen: the four
	// status tabs that overflowed their box at 358px, and the media-type group
	// that could only wrap onto a second row beneath them.
	let {
		open,
		onClose,
		tab,
		onTabChange,
		kind,
		onKindChange,
		counts,
		resultCount,
		activeCount = 0,
		onReset,
	}: {
		open: boolean;
		onClose: () => void;
		tab: RequestTab;
		onTabChange: (t: RequestTab) => void;
		kind: RequestKind;
		onKindChange: (k: RequestKind) => void;
		counts: RequestCounts;
		resultCount: number;
		activeCount?: number;
		onReset: () => void;
	} = $props();

	$effect(() => {
		if (!open) return;
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

	const chip =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff = "border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint";

	let chips = $derived(statusChips(counts));
	const KIND_DOT: Record<string, string> = {
		movies: "grabbing",
		series: "downloading",
	};
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.requests_filter()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{i18n.filter_label()}</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2 pt-3"
			>
				<div class={label}>{i18n.common_status()}</div>
				<div class="flex flex-wrap gap-2">
					{#each chips as c (c.key)}
						{@const on = tab === c.key}
						<button
							type="button"
							aria-pressed={on}
							onclick={() => onTabChange(c.key)}
							class={cn(chip, on ? chipOn : chipOff)}
						>
							{c.label}
							{#if c.count !== undefined}
								<span class="font-mono text-[11px] tabular-nums opacity-70">
									{c.count}
								</span>
							{/if}
						</button>
					{/each}
				</div>

				<div class="pt-5">
					<div class={label}>{i18n.imports_media_type()}</div>
					<div class="flex flex-wrap gap-2">
						{#each KIND_CHIPS as k (k.key)}
							{@const on = kind === k.key}
							<button
								type="button"
								aria-pressed={on}
								onclick={() => onKindChange(k.key)}
								class={cn(chip, on ? chipOn : chipOff)}
							>
								{#if KIND_DOT[k.key]}
									<span
										class="h-1.5 w-1.5 shrink-0 rounded-full"
										style:background-color="var(--status-{KIND_DOT[k.key]})"
										aria-hidden="true"
									></span>
								{/if}
								{k.label}
							</button>
						{/each}
					</div>
				</div>
			</div>

			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
				<button
					type="button"
					disabled={activeCount === 0}
					onclick={onReset}
					class="inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40"
				>
					{i18n.common_reset()}
				</button>
				<button
					type="button"
					onclick={onClose}
					class="inline-flex h-11 flex-1 items-center justify-center rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed"
				>
					Show {resultCount}
				</button>
			</div>
		</div>
	</div>
{/if}
