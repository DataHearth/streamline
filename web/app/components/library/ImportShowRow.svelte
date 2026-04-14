<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
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
	import { api } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import { toast } from "../../lib/toast";
	import type { ImportFileDecision, ImportScanShow } from "../../lib/types";

	type Props = {
		show: ImportScanShow;
		scanId: number;
		reviewing: boolean;
		onChooseMatch: (show: ImportScanShow) => void;
	};

	let { show, scanId, reviewing, onChooseMatch }: Props = $props();

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
		onError: (err) => toast.err(err.message),
	}));

	// Same four buckets as the movie file row, tinted identically.
	const CLASS = {
		confirmed: { label: "Confirmed", kind: "available", Icon: CircleCheckBig },
		ambiguous: { label: "Ambiguous", kind: "wanted", Icon: CircleHelp },
		unmatched: { label: "Unmatched", kind: "paused", Icon: CircleHelp },
		existing: { label: "Existing", kind: "grabbing", Icon: Link2 },
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
			? "Choose match"
			: chosenMatch
				? chosenMatch.year
					? `${chosenMatch.title} (${chosenMatch.year})`
					: chosenMatch.title
				: "Match selected",
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
			? "Restore this show to the import"
			: "Exclude this show from the import"}
		class={cn(
			"inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:opacity-60",
			skipped
				? "bg-surface-2 text-fg"
				: "border border-border bg-bg-card text-fg-muted hover:border-status-failed/40 hover:text-status-failed",
		)}
	>
		{skipped ? "Restore" : "Skip"}
	</button>
{/snippet}

<tr class="transition hover:bg-bg-card">
	<td class="px-4 py-3 align-top">
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
				{show.file_count} file{show.file_count === 1 ? "" : "s"}
			</span>
		</p>
	</td>

	<td class="px-4 py-3 align-top">
		<span
			class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold"
			style:color="var(--status-{cls.kind})"
			style:background-color="color-mix(in srgb, var(--status-{cls.kind}) 14%, transparent)"
		>
			<cls.Icon size={13} aria-hidden="true" />
			{cls.label}
		</span>
	</td>

	<td class="px-4 py-3 align-top">
		{#if show.outcome === "created"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-available"
			>
				<CircleCheckBig size={13} aria-hidden="true" />
				Created
			</span>
		{:else if show.outcome === "failed"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-failed"
				title={show.outcome_message}
			>
				<TriangleAlert size={13} aria-hidden="true" />
				Failed
			</span>
		{:else if show.decision === "accept"}
			<span
				class="inline-flex items-center gap-1 text-xs font-medium text-status-available"
			>
				<ArrowUp size={13} aria-hidden="true" />
				Will adopt
			</span>
		{:else if show.decision === "skip"}
			<span
				class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted"
			>
				<Minus size={13} aria-hidden="true" />
				Will skip
			</span>
		{:else if show.classification === "confirmed"}
			<span class="text-xs text-fg-subtle">Auto-adopt</span>
		{:else if show.classification === "existing"}
			<span class="text-xs text-fg-subtle">Link to show</span>
		{:else}
			<span class="text-xs text-fg-faint">Awaits decision</span>
		{/if}
	</td>

	<td class="px-4 py-3 align-top text-right">
		{#if reviewing && actionable}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(show)}
					title={chosenTvdb != null
						? "Change the matched show"
						: "Pick a TVDB match for this folder"}
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
					Change match
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
