<script lang="ts">
	import {
		ChevronRight,
		CircleCheckBig,
		CircleHelp,
		CircleX,
		LoaderCircle,
		TriangleAlert,
	} from "@lucide/svelte";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import { importModeLabel, importStatusMeta } from "../../lib/imports";
	import type { ImportScan } from "../../lib/types";
	import ProgressBar from "../shared/ProgressBar.svelte";

	let { scan }: { scan: ImportScan } = $props();

	let meta = $derived(importStatusMeta(scan.status));

	const ICONS = {
		running: LoaderCircle,
		committing: LoaderCircle,
		awaiting_review: CircleHelp,
		completed: CircleCheckBig,
		cancelled: CircleX,
		failed: TriangleAlert,
	} as const;
	let Icon = $derived(ICONS[scan.status]);

	let modeLabel = $derived(importModeLabel(scan.mode, scan.import_mode));

	let live = $derived(scan.status === "running");
	let progress = $derived(
		scan.total_count > 0 ? scan.processed_count / scan.total_count : 0,
	);
</script>

<a
	href="/library/imports/{scan.id}"
	class="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 px-5 py-3.5 transition hover:bg-bg-card md:px-6"
>
	<span
		class="grid h-8 w-8 place-items-center rounded-md"
		style:color="var(--status-{meta.kind})"
		style:background-color="color-mix(in srgb, var(--status-{meta.kind}) 14%, transparent)"
	>
		<Icon
			size={16}
			class={meta.live ? "motion-safe:animate-spin" : ""}
			aria-hidden="true"
		/>
	</span>

	<span class="min-w-0">
		<span
			class="block truncate font-mono text-sm text-fg"
			title={scan.source_path}
		>
			{scan.source_path}
		</span>
		<span
			class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-fg-subtle"
		>
			<span
				class="rounded-sm border border-border bg-surface px-1.5 py-px text-[10px] font-medium uppercase tracking-wide text-fg-muted"
			>
				{modeLabel}
			</span>
			<span aria-hidden="true" class="text-fg-faint">·</span>
			<span title={formatDateTime(scan.created_at)}>
				{formatRelative(scan.created_at)}
			</span>
			{#if scan.total_count > 0}
				<span aria-hidden="true" class="text-fg-faint">·</span>
				<span class="font-mono tabular-nums">
					{scan.processed_count}/{scan.total_count} files
				</span>
			{/if}
		</span>
		{#if live && scan.total_count > 0}
			<span class="mt-2 block">
				<ProgressBar
					value={progress}
					status={meta.kind}
					height={1.5}
					shimmer
					label="Scan progress"
				/>
			</span>
		{:else if scan.status === "failed" && scan.failure_reason}
			<span class="mt-1.5 block truncate text-[11px] text-status-failed">
				{scan.failure_reason}
			</span>
		{/if}
	</span>

	<span class="flex items-center gap-3 justify-self-end">
		<span
			class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-semibold tracking-[0.02em]"
			style:background-color="var(--status-{meta.kind})"
			style:color="var(--bg-deep)"
		>
			<span
				class="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--bg-deep)] {meta.live
					? 'motion-safe:animate-pulse'
					: ''}"
				aria-hidden="true"
			></span>
			{meta.label}
		</span>
		<ChevronRight
			size={14}
			class="shrink-0 text-fg-faint"
			aria-hidden="true"
		/>
	</span>
</a>
