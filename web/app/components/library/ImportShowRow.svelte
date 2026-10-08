<script lang="ts" module>
	import { m as i18n } from "@lib/paraglide/messages.js";

	// seriesTypeLabel names a migrated show's Sonarr series type, or null for
	// standard — the default, which is not worth a word on every row. A value
	// this build does not know is shown as sent.
	export function seriesTypeLabel(t: string | undefined): string | null {
		switch (t) {
			case undefined:
			case "":
			case "standard":
				return null;
			case "anime":
				return i18n.lc_anime();
			case "daily":
				return i18n.lc_daily();
			default:
				return t;
		}
	}
</script>

<script lang="ts">
	import { NOUN_FILE } from "@lib/nouns";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import {
		ArrowUp,
		Check,
		CircleCheckBig,
		CircleHelp,
		Eye,
		EyeOff,
		Link2,
		Minus,
		Pencil,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { toast } from "@lib/toast";
	import type {
		ArrApp,
		ImportFileDecision,
		ImportScanShow,
	} from "@lib/types";

	type Props = {
		show: ImportScanShow;
		scanId: number;
		reviewing: boolean;
		// Set on a Sonarr migration: the row then carries the profile, the
		// monitored flag and the series type the show is created with.
		migratedFrom?: ArrApp | undefined;
		onChooseMatch: (show: ImportScanShow) => void;
	};

	let { show, scanId, reviewing, migratedFrom, onChooseMatch }: Props =
		$props();

	let monitoredLabel = $derived(
		show.monitored ? i18n.imports_monitored() : i18n.imports_not_monitored(),
	);
	let seriesType = $derived(seriesTypeLabel(show.series_type));

	const qc = useQueryClient();

	const decide = createMutation<
		ImportScanShow,
		Error,
		{ decision: ImportFileDecision; tvdbId?: number }
	>(() => ({
		mutationFn: ({ decision, tvdbId }) =>
			api<ImportScanShow>(`/library/imports/${scanId}/shows/${show.id}`, {
				method: "PATCH",
				body: tvdbId != null ? { decision, tvdb_id: tvdbId } : { decision },
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["import", scanId, "shows"] });
			qc.invalidateQueries({ queryKey: ["import", scanId, "pending-shows"] });
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// Same four buckets as the movie file row, tinted identically.
	const CLASS = {
		confirmed: { label: i18n.imports_confirmed(), kind: "available", Icon: CircleCheckBig },
		ambiguous: { label: i18n.imports_ambiguous(), kind: "wanted", Icon: CircleHelp },
		unmatched: { label: i18n.imports_unmatched(), kind: "paused", Icon: CircleHelp },
		existing: { label: i18n.imports_existing(), kind: "grabbing", Icon: Link2 },
	} as const;
	let cls = $derived(CLASS[show.classification]);

	// existing shows link to their tracked entry; every other class is resolved
	// by searching TVDB in the picker modal (opened via onChooseMatch).
	let actionable = $derived(
		show.classification === "ambiguous" ||
			show.classification === "unmatched",
	);
	// A match is chosen once decision_tvdb_id is set (cleared again on skip).
	// Resolve its title from the candidate list when possible; a free-search
	// pick has no candidate row, so fall back to a generic label.
	let chosenTvdb = $derived(show.decision_tvdb_id);
	let chosenMatch = $derived(
		chosenTvdb != null
			? (show.candidates ?? []).find((c) => c.tvdb_id === chosenTvdb)
			: undefined,
	);
	let chosenLabel = $derived(
		chosenTvdb == null
			? i18n.action_choose_match()
			: chosenMatch
				? chosenMatch.year
					? `${chosenMatch.title} (${chosenMatch.year})`
					: chosenMatch.title
				: i18n.imports_match_selected(),
	);

	let skipped = $derived(show.decision === "skip");
	function toggleSkip() {
		decide.mutate({ decision: skipped ? "pending" : "skip" });
	}
</script>

{#snippet skipToggle()}
	<button
		type="button"
		disabled={decide.isPending}
		onclick={toggleSkip}
		aria-pressed={skipped}
		title={skipped
			? i18n.imports_restore_show()
			: i18n.imports_exclude_show()}
		class={cn(
			"inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60",
			skipped
				? "bg-surface-2 text-fg"
				: "border border-border bg-bg-card text-fg-muted hover:border-status-failed/40 hover:text-status-failed",
		)}
	>
		{skipped ? i18n.common_restore() : i18n.common_skip()}
	</button>
{/snippet}

<tr class="transition hover:bg-bg-card">
	<td class="hidden px-4 py-3 align-top md:table-cell">
		<p
			class="break-all font-mono text-[13px] text-fg"
			title={show.folder_path}
		>
			{show.folder_path}
		</p>
		<p
			class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-fg-subtle"
		>
			{#if show.parsed_title}
				<span>
					{show.parsed_title}{#if show.parsed_year}{" "}<span
							class="text-fg-muted">({show.parsed_year})</span
						>{/if}
				</span>
				<span aria-hidden="true" class="text-fg-faint">·</span>
			{/if}
			<span class="font-mono tabular-nums">
				{NOUN_FILE.count(show.file_count)}
			</span>
			{#if migratedFrom}
				<span aria-hidden="true" class="text-fg-faint">·</span>
				{#if show.quality_profile}
					<span class="text-fg-muted" title={i18n.quality_profile()}>
						{show.quality_profile}
					</span>
					<span aria-hidden="true" class="text-fg-faint">·</span>
				{/if}
				{#if seriesType}
					<span class="text-fg-muted">{seriesType}</span>
					<span aria-hidden="true" class="text-fg-faint">·</span>
				{/if}
				<span
					role="img"
					aria-label={monitoredLabel}
					title={monitoredLabel}
					class={cn(
						"inline-flex",
						show.monitored ? "text-fg-muted" : "text-fg-faint",
					)}
				>
					{#if show.monitored}
						<Eye size={13} aria-hidden="true" />
					{:else}
						<EyeOff size={13} aria-hidden="true" />
					{/if}
				</span>
			{/if}
		</p>
		<span
			class="mt-1.5 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold md:hidden"
			style:color="var(--status-{cls.kind})"
			style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
		>
			<cls.Icon size={13} aria-hidden="true" />
			{cls.label}
		</span>
	</td>

	<td class="hidden px-4 py-3 align-top md:table-cell">
		<span
			class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold"
			style:color="var(--status-{cls.kind})"
			style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
			title={show.classification === "existing"
				? i18n.imports_show_exists()
				: undefined}
		>
			<cls.Icon size={13} aria-hidden="true" />
			{cls.label}
		</span>
	</td>

	<td class="hidden px-4 py-3 align-top md:table-cell">
		{#if show.outcome === "created"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-available"
			>
				<CircleCheckBig size={13} aria-hidden="true" />
				{i18n.common_created()}
			</span>
		{:else if show.outcome === "attached"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-available"
			>
				<Link2 size={13} aria-hidden="true" />
				{i18n.imports_attached()}
			</span>
		{:else if show.outcome === "failed"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-failed"
				title={show.outcome_message}
			>
				<TriangleAlert size={13} aria-hidden="true" />
				{i18n.status_failed()}
			</span>
		{:else if show.decision === "accept"}
			<span
				class="inline-flex items-center gap-1 text-xs font-medium text-status-available"
			>
				<ArrowUp size={13} aria-hidden="true" />
				{i18n.imports_will_adopt()}
			</span>
		{:else if show.decision === "skip"}
			<span
				class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted"
			>
				<Minus size={13} aria-hidden="true" />
				{i18n.imports_will_skip()}
			</span>
		{:else if show.classification === "confirmed"}
			<span class="text-xs text-fg-subtle">{i18n.imports_auto_adopt()}</span>
		{:else if show.classification === "existing"}
			<span class="text-xs text-fg-subtle">{i18n.imports_link_to_show()}</span>
		{:else}
			<span class="text-xs text-fg-faint">{i18n.imports_awaits_decision()}</span>
		{/if}
	</td>

	<td class="px-4 py-3 align-top text-right">
		{#if reviewing && actionable}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(show)}
					title={chosenTvdb != null
						? i18n.imports_change_matched_show()
						: i18n.imports_pick_tvdb()}
					class="inline-flex max-w-[12rem] items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {chosenTvdb !=
					null
						? 'bg-status-available/15 text-status-available'
						: 'border border-border bg-bg-card text-fg-muted hover:border-accent/40 hover:text-fg'}"
				>
					{#if chosenTvdb != null}
						<Check size={13} class="shrink-0" aria-hidden="true" />
					{:else}
						<Pencil size={13} class="shrink-0" aria-hidden="true" />
					{/if}
					<span class="truncate">{chosenLabel}</span>
				</button>
				{@render skipToggle()}
			</div>
		{:else if reviewing && show.classification === "confirmed"}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(show)}
					class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-card px-2.5 py-1 text-xs font-medium text-fg-muted transition hover:border-accent/40 hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
				>
					<Pencil size={12} aria-hidden="true" />
					{i18n.imports_change_match()}
				</button>
				{@render skipToggle()}
			</div>
		{:else if reviewing && show.classification === "existing"}
			{@render skipToggle()}
		{:else}
			<span class="font-mono text-xs text-fg-faint">—</span>
		{/if}
	</td>
</tr>
