<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { Check, Search } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import {
		CLASS_META,
		isActionable,
		outcomeWord,
		shortId,
		unitText,
		type MatchId,
		type TouchEntry,
	} from "@lib/imports-touch";
	import { IMPORT_KIND } from "@lib/imports";
	import type { ImportScanKind } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The decision surface for one file / show folder below lg. The row's job is
	// to say which entry this is and whether it needs you; every action lives
	// here, so the list stays a list.
	let {
		entry,
		kind = "movie",
		reviewing,
		busy = false,
		onClose,
		onPick,
		onSearch,
		onSkipToggle,
	}: {
		entry: TouchEntry | null;
		kind?: ImportScanKind;
		reviewing: boolean;
		busy?: boolean;
		onClose: () => void;
		onPick: (entry: TouchEntry, candidateId: MatchId) => void;
		onSearch: (entry: TouchEntry) => void;
		onSkipToggle: (entry: TouchEntry) => void;
	} = $props();

	// Mounted with the page; open while it renders an entry. A watcher armed while closed
	// would take the phone's Back from the page.

	let cls = $derived(entry ? CLASS_META[entry.classification] : null);
	let word = $derived(entry ? outcomeWord(entry, kind === "series") : null);
	let skipped = $derived(entry?.decision === "skip");
	let actionable = $derived(entry ? isActionable(entry.classification) : false);
	// confirmed rows can still be re-pointed at a different title; existing rows
	// are already bound to a library entry, so skip is their only lever.
	let canPick = $derived(
		entry != null &&
			reviewing &&
			(actionable || entry.classification === "confirmed"),
	);
	let source = $derived(IMPORT_KIND[kind].source);
	let text = $derived(unitText(kind));
</script>

{#if entry && cls && word}
	{#snippet sheetFooter()}
					<button
						type="button"
						disabled={busy}
						onclick={() => onSkipToggle(entry)}
						class="inline-flex h-11 flex-1 items-center justify-center rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
					>
						{skipped ? i18n.common_restore() : text.skipThis}
					</button>
					{#if entry.chosenId != null}
						<button
							type="button"
							onclick={onClose}
							class="inline-flex h-11 flex-[1.4] items-center justify-center gap-2 rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed"
						>
							<Check size={15} aria-hidden="true" />
							<span class="truncate">{i18n.common_use_label({ label: entry.chosenLabel ?? "" })}</span>
						</button>
					{/if}
	{/snippet}

	<BottomSheet
		label={text.decide}
		{onClose}
		headerClass="items-start pb-3"
		bodyClass="pt-4"
		footer={reviewing ? sheetFooter : undefined}
	>
		{#snippet header()}
			<div class="min-w-0 pt-1">
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">
					{entry.heading}
				</h2>
				<p class="mt-1 break-all font-mono text-[11px] leading-relaxed text-fg-subtle">
					{entry.path}
				</p>
			</div>
		{/snippet}
		{#snippet subheader()}
			<div class="flex flex-wrap items-center gap-2 border-b border-border px-5 pb-3.5">
				<span
					class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold"
					style:color="var(--status-{cls.kind})"
					style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
				>
					{cls.label}
				</span>
				{#if entry.flag}
					<span class="rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-semibold text-accent-text">
						{entry.flag}
					</span>
				{/if}
				<span
					class="max-w-full truncate rounded-full bg-surface px-2 py-0.5 font-mono text-[11px] text-fg-muted"
				>
					{entry.sub}
				</span>
				{#if entry.outcomeMessage}
					<span
						class="w-full text-[11.5px] {entry.outcome === 'failed'
							? 'text-status-failed'
							: 'text-fg-muted'}"
					>
						{entry.outcomeMessage}
					</span>
				{/if}
			</div>
		{/snippet}
		{#if canPick}
			{#if entry.candidates.length > 0}
				<div
					class="mb-2.5 eyebrow text-[9.5px] text-fg-subtle"
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
							class={twMerge(
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
								<span class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle">
									{#if c.sub}{c.sub}{" · "}{/if}{c.year ?? "—"}{#if c.tag}{" · "}{c.tag}{/if} · {source} {shortId(c.id)}
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
					{text.nothingParsed}
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
					{text.alreadyMatched}
				{/if}
			</p>
		{/if}
	</BottomSheet>
{/if}
