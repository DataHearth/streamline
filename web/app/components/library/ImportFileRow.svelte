<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import {
		ArrowUp,
		Check,
		CircleCheckBig,
		CircleHelp,
		CircleX,
		Eye,
		EyeOff,
		Link2,
		Minus,
		Pencil,
		TriangleAlert,
	} from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { appLabel } from "@lib/arr-import";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import { isTitleOnly } from "@lib/imports";
	import { toast } from "@lib/toast";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import type {
		ArrApp,
		ImportFileDecision,
		ImportScanFile,
	} from "@lib/types";

	type Props = {
		file: ImportScanFile;
		scanId: number;
		reviewing: boolean;
		// Set on a Radarr migration: the row then carries the profile and the
		// monitored flag the title is created with.
		migratedFrom?: ArrApp | undefined;
		onChooseMatch: (file: ImportScanFile) => void;
	};

	let { file, scanId, reviewing, migratedFrom, onChooseMatch }: Props =
		$props();

	// A title the source tracks without a file has no path and no size; the
	// title and a badge saying so stand where the path would be.
	let titleOnly = $derived(isTitleOnly(file));
	let monitoredLabel = $derived(
		file.monitored ? i18n.imports_monitored() : i18n.imports_not_monitored(),
	);

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
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	// confirmed / existing are decided by the parser; ambiguous / unmatched
	// are the only classes the reviewer can act on.
	const CLASS = {
		confirmed: { label: i18n.imports_confirmed(), kind: "available", Icon: CircleCheckBig },
		ambiguous: { label: i18n.imports_ambiguous(), kind: "wanted", Icon: CircleHelp },
		unmatched: { label: i18n.imports_unmatched(), kind: "paused", Icon: CircleHelp },
		existing: { label: i18n.imports_existing(), kind: "grabbing", Icon: Link2 },
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
			? i18n.action_choose_match()
			: (chosenMatch?.title ?? i18n.imports_match_selected()),
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
			? i18n.imports_restore_file()
			: i18n.imports_exclude_file()}
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
		{#if titleOnly}
			<p class="flex flex-wrap items-center gap-x-2 gap-y-1">
				<span class="text-[13px] font-medium text-fg">
					{file.parsed_title}{#if file.parsed_year}<span class="text-fg-muted"> ({file.parsed_year})</span
						>{/if}
				</span>
				<span
					class="rounded-sm border border-status-wanted/30 bg-status-wanted/10 px-1.5 py-px text-[10px] font-medium uppercase tracking-wide text-status-wanted"
					title={migratedFrom
						? i18n.imports_title_only_help({ app: appLabel(migratedFrom) })
						: undefined}
				>
					{i18n.imports_title_only()}
				</span>
			</p>
		{:else}
			<p
				class="break-all font-mono text-[13px] text-fg"
				title={file.source_path}
			>
				{file.source_path}
			</p>
		{/if}
		<p class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-fg-subtle">
			{#if !titleOnly}
				<span class="font-mono tabular-nums">{formatBytes(file.size)}</span>
				{#if file.parsed_title}
					<span aria-hidden="true" class="text-fg-faint">·</span>
					<span>
						{i18n.imports_parsed()} <span class="text-fg-muted">{file.parsed_title}</span
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
			{/if}
			{#if migratedFrom}
				{#if !titleOnly}
					<span aria-hidden="true" class="text-fg-faint">·</span>
				{/if}
				{#if file.quality_profile}
					<span class="text-fg-muted" title={i18n.quality_profile()}>
						{file.quality_profile}
					</span>
					<span aria-hidden="true" class="text-fg-faint">·</span>
				{/if}
				<span
					role="img"
					aria-label={monitoredLabel}
					title={monitoredLabel}
					class={cn(
					"inline-flex",
					file.monitored ? "text-fg-muted" : "text-fg-faint",
				)}
				>
					{#if file.monitored}
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
			title={file.classification === "existing"
				? i18n.imports_movie_exists()
				: undefined}
		>
			<cls.Icon size={13} aria-hidden="true" />
			{cls.label}
		</span>
	</td>

	<td class="hidden px-4 py-3 align-top md:table-cell">
		{#if file.outcome === "created"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<CircleCheckBig size={13} aria-hidden="true" />
				{i18n.common_created()}
			</span>
		{:else if file.outcome === "attached"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-status-available">
				<Link2 size={13} aria-hidden="true" />
				{i18n.imports_attached()}
			</span>
		{:else if file.outcome === "skipped"}
			<span class="inline-flex items-center gap-1 text-xs font-semibold text-fg-muted">
				<CircleX size={13} aria-hidden="true" />
				{i18n.common_skipped()}
			</span>
		{:else if file.outcome === "failed"}
			<span
				class="inline-flex items-center gap-1 text-xs font-semibold text-status-failed"
				title={file.outcome_message}
			>
				<TriangleAlert size={13} aria-hidden="true" />
				{i18n.status_failed()}
			</span>
		{:else if file.decision === "accept"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-status-available">
				<ArrowUp size={13} aria-hidden="true" />
				{i18n.imports_will_accept()}
			</span>
		{:else if file.decision === "skip"}
			<span class="inline-flex items-center gap-1 text-xs font-medium text-fg-muted">
				<Minus size={13} aria-hidden="true" />
				{i18n.imports_will_skip()}
			</span>
		{:else if file.classification === "confirmed"}
			<span class="text-xs text-fg-subtle">{i18n.imports_auto_accept()}</span>
		{:else if file.classification === "existing"}
			<span class="text-xs text-fg-subtle">{i18n.imports_attach_to_library()}</span>
		{:else}
			<span class="text-xs text-fg-faint">{i18n.imports_awaits_decision()}</span>
		{/if}
	</td>

	<td class="px-4 py-3 align-top text-right">
		{#if reviewing && actionable}
			<div class="inline-flex items-center gap-1.5">
				<button
					type="button"
					onclick={() => onChooseMatch(file)}
					title={chosenTmdb != null
						? i18n.imports_change_matched_movie()
						: i18n.imports_pick_tmdb()}
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
					{i18n.imports_change_match()}
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
