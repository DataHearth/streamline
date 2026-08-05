<script lang="ts">
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Search, Trash2, X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";

	// Everything the activity toolbars can't afford to keep on screen below lg:
	// the release filter, the sort the torrent table's headers used to own, and
	// Clear completed. The status set is not here — the toolbar keeps those
	// pills on screen at every width, so repeating them was two of the same
	// control.
	let {
		open,
		onClose,
		title = "Filter",
		search,
		onSearchChange,
		searchPlaceholder = "Filter title or release…",
		sortChips,
		sortKey = null,
		onSortChange,
		onClearCompleted,
		clearableCount = 0,
		onReset,
		activeCount = 0,
	}: {
		open: boolean;
		onClose: () => void;
		title?: string;
		search: string;
		onSearchChange: (q: string) => void;
		searchPlaceholder?: string;
		sortChips?: { key: string; label: string }[];
		sortKey?: string | null;
		onSortChange?: (key: string) => void;
		onClearCompleted?: () => void;
		clearableCount?: number;
		onReset: () => void;
		activeCount?: number;
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
		"inline-flex h-9 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff = "border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint";
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label="Filter and sort"
	>
		<button
			type="button"
			aria-label="Close"
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
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{title}</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label="Close"
					class="grid h-9 w-9 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2"
			>
				<div
					class="flex h-11 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
				>
					<Search class="h-4 w-4 shrink-0 text-fg-subtle" aria-hidden="true" />
					<input
						type="search"
						value={search}
						oninput={(e) => onSearchChange(e.currentTarget.value)}
						placeholder={searchPlaceholder}
						class="min-w-0 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-fg-faint"
					/>
					{#if search}
						<button
							type="button"
							onclick={() => onSearchChange("")}
							aria-label="Clear search"
							class="grid h-7 w-7 shrink-0 place-items-center rounded-full text-fg-faint transition active:bg-surface"
						>
							<X size={14} aria-hidden="true" />
						</button>
					{/if}
				</div>

				{#if sortChips && onSortChange}
					<div class="pt-5">
						<div class={label}>Sort</div>
						<div class="flex flex-wrap gap-2">
							{#each sortChips as opt (opt.key)}
								{@const active = sortKey === opt.key}
								<button
									type="button"
									aria-pressed={active}
									onclick={() => onSortChange?.(opt.key)}
									class={cn(chip, active ? chipOn : chipOff)}
								>
									{opt.label}
								</button>
							{/each}
						</div>
					</div>
				{/if}
			</div>

			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
				{#if onClearCompleted}
					<button
						type="button"
						disabled={clearableCount === 0}
						onclick={() => {
							onClearCompleted?.();
							onClose();
						}}
						class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-40"
					>
						<Trash2 size={16} aria-hidden="true" />
						Clear completed{clearableCount > 0 ? ` (${clearableCount})` : ""}
					</button>
				{/if}
				<button
					type="button"
					disabled={activeCount === 0}
					onclick={onReset}
					class={cn(
						"inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40",
						!onClearCompleted && "flex-1 border border-border bg-surface",
					)}
				>
					Reset
				</button>
			</div>
		</div>
	</div>
{/if}
