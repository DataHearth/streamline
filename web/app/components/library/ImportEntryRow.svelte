<script lang="ts">
	import {
		ArrowUp,
		Check,
		CircleCheckBig,
		CircleHelp,
		Link2,
		Minus,
		Pencil,
		TriangleAlert,
	} from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { IMPORT_KIND } from "@lib/imports";
	import { unitText, type TouchEntry } from "@lib/imports-touch";
	import type { ImportScanKind } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The desktop review row for album folders and books: ImportShowRow's four
	// columns, read off the same normalised entry the touch list uses. The route
	// owns the mutations, so this row only says what was found and hands the two
	// decisions back.
	type Props = {
		entry: TouchEntry;
		kind: ImportScanKind;
		reviewing: boolean;
		busy?: boolean;
		onChooseMatch: (entry: TouchEntry) => void;
		onSkipToggle: (entry: TouchEntry) => void;
	};
	let { entry, kind, reviewing, busy = false, onChooseMatch, onSkipToggle }: Props = $props();

	const CLASS = {
		confirmed: { label: i18n.imports_confirmed(), kind: "available", Icon: CircleCheckBig },
		ambiguous: { label: i18n.imports_ambiguous(), kind: "wanted", Icon: CircleHelp },
		unmatched: { label: i18n.imports_unmatched(), kind: "paused", Icon: CircleHelp },
		existing: { label: i18n.imports_existing(), kind: "grabbing", Icon: Link2 },
	} as const;
	let cls = $derived(CLASS[entry.classification]);
	let text = $derived(unitText(kind));
	let source = $derived(IMPORT_KIND[kind].source);
	let actionable = $derived(entry.classification === "ambiguous" || entry.classification === "unmatched");
	let chosen = $derived(entry.chosenId != null);
	let skipped = $derived(entry.decision === "skip");

	const pill = "inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60";
</script>

{#snippet skipToggle()}
	<button
		type="button"
		disabled={busy}
		onclick={() => onSkipToggle(entry)}
		aria-pressed={skipped}
		title={skipped ? text.restore : text.exclude}
		class={cn(
			pill,
			skipped
				? "bg-surface-2 text-fg"
				: "border border-border bg-bg-card text-fg-muted hover:border-status-failed/40 hover:text-status-failed",
		)}
	>
		{skipped ? i18n.common_restore() : i18n.common_skip()}
	</button>
{/snippet}

<tr class="transition hover:bg-bg-card">
	<td class="px-4 py-3 align-top">
		<p class="break-all font-mono text-[13px] text-fg" title={entry.path}>{entry.path}</p>
		<p class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-fg-subtle">
			{#if !entry.headingWeak}
				<span class="text-fg-muted">{entry.heading}</span>
				<span aria-hidden="true" class="text-fg-faint">·</span>
			{/if}
			<span class="font-mono">{entry.sub}</span>
			{#if entry.flag}
				<span class="rounded-full bg-accent-soft px-2 py-px text-[10.5px] font-semibold text-accent-text">{entry.flag}</span>
			{/if}
		</p>
	</td>

	<td class="px-4 py-3 align-top">
		<span
			class="inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-semibold"
			style:color="var(--status-{cls.kind})"
			style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
			title={entry.classification === "existing" ? text.exists : undefined}
		>
			<cls.Icon size={13} aria-hidden="true" />
			{cls.label}
		</span>
	</td>

	<td class="px-4 py-3 align-top whitespace-nowrap">
		{#if entry.outcome === "created"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<CircleCheckBig size={13} aria-hidden="true" />
				{i18n.common_created()}
			</span>
		{:else if entry.outcome === "attached"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<Link2 size={13} aria-hidden="true" />
				{i18n.imports_attached()}
			</span>
		{:else if entry.outcome === "skipped"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted">
				<Minus size={13} aria-hidden="true" />
				{i18n.common_skipped()}
			</span>
		{:else if entry.outcome === "failed"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-failed" title={entry.outcomeMessage}>
				<TriangleAlert size={13} aria-hidden="true" />
				{i18n.status_failed()}
			</span>
		{:else if entry.decision === "accept"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-status-available">
				<ArrowUp size={13} aria-hidden="true" />
				{i18n.imports_will_accept()}
			</span>
		{:else if skipped}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted">
				<Minus size={13} aria-hidden="true" />
				{i18n.imports_will_skip()}
			</span>
		{:else if entry.classification === "confirmed"}
			<span class="text-xs text-fg-subtle">{i18n.imports_accept()}</span>
		{:else if entry.classification === "existing"}
			<span class="text-xs text-fg-subtle">{i18n.imports_attach()}</span>
		{:else}
			<span class="text-xs text-fg-faint">{i18n.imports_awaits_decision()}</span>
		{/if}
	</td>

	<td class="px-4 py-3 text-right align-top">
		{#if reviewing && (actionable || entry.classification === "confirmed")}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(entry)}
					title={i18n.imports_search_source({ source })}
					class={cn(
						pill,
						"max-w-[13rem]",
						chosen
							? "bg-status-available/15 text-status-available"
							: actionable
								? "border border-border bg-bg-card text-fg-muted hover:border-accent/40 hover:text-fg"
								: "border border-border bg-bg-card font-medium text-fg-muted hover:border-accent/40 hover:text-fg",
					)}
				>
					{#if chosen}
						<Check size={13} class="shrink-0" aria-hidden="true" />
					{:else}
						<Pencil size={13} class="shrink-0" aria-hidden="true" />
					{/if}
					<span class="truncate">
						{chosen ? entry.chosenLabel : actionable ? i18n.action_choose_match() : i18n.imports_change_match()}
					</span>
				</button>
				{@render skipToggle()}
			</div>
		{:else if reviewing && entry.classification === "existing"}
			{@render skipToggle()}
		{:else}
			<span class="font-mono text-xs text-fg-faint">—</span>
		{/if}
	</td>
</tr>
