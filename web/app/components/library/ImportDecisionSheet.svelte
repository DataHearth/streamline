<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Check, Search, X } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import {
		CLASS_META,
		isActionable,
		outcomeWord,
		type TouchEntry,
	} from "../../lib/imports-touch";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The decision surface for one file / show folder below lg. The row's job is
	// to say which entry this is and whether it needs you; every action lives
	// here, so the list stays a list.
	let {
		entry,
		series = false,
		reviewing,
		busy = false,
		onClose,
		onPick,
		onSearch,
		onSkipToggle,
	}: {
		entry: TouchEntry | null;
		series?: boolean;
		reviewing: boolean;
		busy?: boolean;
		onClose: () => void;
		onPick: (entry: TouchEntry, candidateId: number) => void;
		onSearch: (entry: TouchEntry) => void;
		onSkipToggle: (entry: TouchEntry) => void;
	} = $props();

	$effect(() => {
		if (!entry) return;
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

	let cls = $derived(entry ? CLASS_META[entry.classification] : null);
	let word = $derived(entry ? outcomeWord(entry, series) : null);
	let skipped = $derived(entry?.decision === "skip");
	let actionable = $derived(entry ? isActionable(entry.classification) : false);
	// confirmed rows can still be re-pointed at a different title; existing rows
	// are already bound to a library entry, so skip is their only lever.
	let canPick = $derived(
		entry != null &&
			reviewing &&
			(actionable || entry.classification === "confirmed"),
	);
	let source = $derived(series ? "TVDB" : "TMDB");
	let noun = $derived(series ? "show" : "file");
</script>

{#if entry && cls && word}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={series ? i18n.imports_decide_show() : i18n.imports_decide_file()}
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
				class="relative flex cursor-grab touch-none select-none items-start justify-between gap-3 px-5 pb-3 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<div class="min-w-0 pt-1">
					<h2 class="text-[17px] font-semibold tracking-tight text-fg">
						{entry.heading}
					</h2>
					<p class="mt-1 break-all font-mono text-[11px] leading-relaxed text-fg-subtle">
						{entry.path}
					</p>
				</div>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div class="flex flex-wrap items-center gap-2 border-b border-border px-5 pb-3.5">
				<span
					class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold"
					style:color="var(--status-{cls.kind})"
					style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
				>
					{cls.label}
				</span>
				<span
					class="rounded-full bg-surface px-2 py-0.5 font-mono text-[11px] text-fg-muted"
				>
					{entry.sub}
				</span>
				{#if entry.outcome === "failed" && entry.outcomeMessage}
					<span class="w-full text-[11.5px] text-status-failed">
						{entry.outcomeMessage}
					</span>
				{/if}
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2 pt-4"
			>
				{#if canPick}
					{#if entry.candidates.length > 0}
						<div
							class="mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
						>
							{i18n.imports_candidates_from({ source })}
						</div>
						<div class="flex flex-col gap-2">
							{#each entry.candidates as c (c.id)}
								{@const on = entry.chosenId === c.id}
								<button
									type="button"
									disabled={busy}
									onclick={() => onPick(entry, c.id)}
									class={cn(
										"flex items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition disabled:opacity-60",
										on
											? "border-accent bg-accent-soft"
											: "border-border bg-bg-card active:bg-surface",
									)}
								>
									<span class="min-w-0 flex-1">
										<span class="block truncate text-[13px] font-semibold text-fg">
											{c.title}
										</span>
										<span class="mt-0.5 block font-mono text-[10.5px] text-fg-subtle">
											{c.year ?? "—"} · {source} {c.id}
										</span>
									</span>
									{#if on}
										<Check size={16} class="shrink-0 text-accent-text" aria-hidden="true" />
									{/if}
								</button>
							{/each}
						</div>
					{:else}
						<p class="text-[13px] text-fg-muted">
							{series
								? i18n.imports_nothing_parsed_show()
								: i18n.imports_nothing_parsed_file()}
						</p>
					{/if}

					<!-- Deliberately a fallback: the candidate list answers most entries,
					     and this opens the full lookup screen seeded with the parsed title. -->
					<button
						type="button"
						onclick={() => onSearch(entry)}
						class="mt-3 flex w-full items-center gap-2.5 rounded-xl border border-dashed border-border-strong px-3 py-2.5 text-left text-[13px] font-medium text-fg-muted transition active:bg-surface"
					>
						<Search size={16} class="shrink-0" aria-hidden="true" />
						<span class="flex-1">
							{entry.candidates.length > 0
								? i18n.imports_none_of_these()
								: i18n.common_search()} — {i18n.imports_search_provider({ source })}
						</span>
					</button>
				{:else}
					<p class="text-[13px] text-fg-muted">
						{#if !reviewing}
							{i18n.imports_no_longer_review()}
						{:else}
							{series
								? i18n.imports_already_matched_show()
								: i18n.imports_already_matched_file()}
						{/if}
					</p>
				{/if}
			</div>

			{#if reviewing}
				<div
					class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
				>
					<button
						type="button"
						disabled={busy}
						onclick={() => onSkipToggle(entry)}
						class="inline-flex h-11 flex-1 items-center justify-center rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
					>
						{skipped
							? i18n.common_restore()
							: series
								? i18n.imports_skip_this_show()
								: i18n.imports_skip_this_file()}
					</button>
					{#if entry.chosenId != null}
						<button
							type="button"
							onclick={onClose}
							class="inline-flex h-11 flex-[1.4] items-center justify-center gap-2 rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed"
						>
							<Check size={15} aria-hidden="true" />
							<span class="truncate">Use {entry.chosenLabel}</span>
						</button>
					{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}
