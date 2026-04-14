<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import {
		ArrowUp,
		Check,
		CircleCheckBig,
		CircleHelp,
		CircleX,
		Link2,
		Minus,
		Pencil,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import { formatBytes } from "../../lib/format";
	import { toast } from "../../lib/toast";
	import type {
		ImportFileDecision,
		ImportScanFile,
	} from "../../lib/types";

	type Props = {
		file: ImportScanFile;
		scanId: number;
		reviewing: boolean;
		onChooseMatch: (file: ImportScanFile) => void;
	};

	let { file, scanId, reviewing, onChooseMatch }: Props = $props();

	const qc = useQueryClient();

	const decide = createMutation<ImportScanFile, Error, ImportFileDecision>(
		() => ({
			mutationFn: (decision) =>
				api<ImportScanFile>(
					`/library/imports/${scanId}/files/${file.id}`,
					{ method: "PATCH", body: { decision } },
				),
			onSuccess: () => {
				qc.invalidateQueries({
					queryKey: ["import", scanId, "files"],
				});
				qc.invalidateQueries({
					queryKey: ["import", scanId, "pending"],
				});
			},
			onError: (err) => toast.err(err.message),
		}),
	);

	// confirmed / existing are decided by the parser; ambiguous / unmatched
	// are the only classes the reviewer can act on.
	const CLASS = {
		confirmed: { label: "Confirmed", kind: "available", Icon: CircleCheckBig },
		ambiguous: { label: "Ambiguous", kind: "wanted", Icon: CircleHelp },
		unmatched: { label: "Unmatched", kind: "paused", Icon: CircleHelp },
		existing: { label: "Existing", kind: "grabbing", Icon: Link2 },
	} as const;
	let cls = $derived(CLASS[file.classification]);
	let actionable = $derived(
		file.classification === "ambiguous" ||
			file.classification === "unmatched",
	);
	// A match is chosen once decision_tmdb_id is set (cleared again on skip).
	// Resolve its title from the candidate list when possible; a free-search
	// pick has no candidate row, so fall back to a generic label.
	let chosenTmdb = $derived(file.decision_tmdb_id);
	let chosenMatch = $derived(
		chosenTmdb != null
			? (file.candidates ?? []).find((c) => c.tmdb_id === chosenTmdb)
			: undefined,
	);
	let chosenLabel = $derived(
		chosenTmdb == null
			? "Choose match"
			: (chosenMatch?.title ?? "Match selected"),
	);

	// Skip is a toggle: skip excludes the file from commit, restore returns it
	// to pending (which auto-commits again for confirmed/existing matches).
	let skipped = $derived(file.decision === "skip");
	function toggleSkip() {
		decide.mutate(skipped ? "pending" : "skip");
	}
</script>

{#snippet skipToggle()}
	<button
		type="button"
		disabled={decide.isPending}
		onclick={toggleSkip}
		aria-pressed={skipped}
		title={skipped
			? "Restore this file to the import"
			: "Exclude this file from the import"}
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
			title={file.source_path}
		>
			{file.source_path}
		</p>
		<p class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-fg-subtle">
			<span class="font-mono tabular-nums">{formatBytes(file.size)}</span>
			{#if file.parsed_title}
				<span aria-hidden="true" class="text-fg-faint">·</span>
				<span>
					Parsed: <span class="text-fg-muted">{file.parsed_title}</span
					>{#if file.parsed_year}<span class="text-fg-muted"> ({file.parsed_year})</span
						>{/if}
				</span>
				{#if file.parsed_quality}
					<span aria-hidden="true" class="text-fg-faint">·</span>
					<span class="font-mono">{file.parsed_quality}</span>
				{/if}
				{#if file.parsed_release_group}
					<span aria-hidden="true" class="text-fg-faint">·</span>
					<span>{file.parsed_release_group}</span>
				{/if}
			{/if}
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
		{#if file.outcome === "created"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<CircleCheckBig size={13} aria-hidden="true" />
				Created
			</span>
		{:else if file.outcome === "attached"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<Link2 size={13} aria-hidden="true" />
				Attached
			</span>
		{:else if file.outcome === "skipped"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-fg-muted">
				<CircleX size={13} aria-hidden="true" />
				Skipped
			</span>
		{:else if file.outcome === "failed"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-failed"
				title={file.outcome_message}
			>
				<TriangleAlert size={13} aria-hidden="true" />
				Failed
			</span>
		{:else if file.decision === "accept"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-status-available">
				<ArrowUp size={13} aria-hidden="true" />
				Will accept
			</span>
		{:else if file.decision === "skip"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted">
				<Minus size={13} aria-hidden="true" />
				Will skip
			</span>
		{:else if file.classification === "confirmed"}
			<span class="text-xs text-fg-subtle">Auto-accept</span>
		{:else if file.classification === "existing"}
			<span class="text-xs text-fg-subtle">Attach to library</span>
		{:else}
			<span class="text-xs text-fg-faint">Awaits decision</span>
		{/if}
	</td>

	<td class="px-4 py-3 align-top text-right">
		{#if reviewing && actionable}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(file)}
					title={chosenTmdb != null
						? "Change the matched movie"
						: "Pick a TMDB match for this file"}
					class="inline-flex max-w-[12rem] items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {chosenTmdb !=
					null
						? 'bg-status-available/15 text-status-available'
						: 'border border-border bg-bg-card text-fg-muted hover:border-accent/40 hover:text-fg'}"
				>
					{#if chosenTmdb != null}
						<Check size={13} class="shrink-0" aria-hidden="true" />
					{:else}
						<Pencil size={13} class="shrink-0" aria-hidden="true" />
					{/if}
					<span class="truncate">{chosenLabel}</span>
				</button>
				{@render skipToggle()}
			</div>
		{:else if reviewing && file.classification === "confirmed"}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(file)}
					class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-card px-2.5 py-1 text-xs font-medium text-fg-muted transition hover:border-accent/40 hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
				>
					<Pencil size={12} aria-hidden="true" />
					Change match
				</button>
				{@render skipToggle()}
			</div>
		{:else if reviewing && file.classification === "existing"}
			{@render skipToggle()}
		{:else}
			<span class="font-mono text-xs text-fg-faint">—</span>
		{/if}
	</td>
</tr>
