<script lang="ts" module>
	import type { TouchEntry } from "@lib/imports-touch";

	// What a migrated row is created with, carried beside the shared touch
	// entry: the route fills it only for a Radarr/Sonarr scan.
	export type MigrationFacts = {
		qualityProfile?: string | undefined;
		monitored: boolean;
		seriesType?: string | undefined;
	};
	export type ReviewEntry = TouchEntry & { migration?: MigrationFacts };
</script>

<script lang="ts">
	import { ChevronRight, Eye, EyeOff } from "@lucide/svelte";
	import { appLabel } from "@lib/arr-import";
	import { cn } from "@lib/cn";
	import { isTitleOnly } from "@lib/imports";
	import { outcomeWord } from "@lib/imports-touch";
	import type { ArrApp, ImportScanKind } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { seriesTypeLabel } from "./ImportShowRow.svelte";

	let {
		entry,
		kind = "movie",
		wide = false,
		migratedFrom,
		onOpen,
	}: {
		entry: ReviewEntry;
		kind?: ImportScanKind;
		// From md up the row keeps its shape and gains two trailing columns
		// instead of the chevron — same component at 390 and at 834.
		wide?: boolean;
		migratedFrom?: ArrApp | undefined;
		onOpen: (entry: ReviewEntry) => void;
	} = $props();

	let word = $derived(outcomeWord(entry, kind === "series"));
	// fileEntry leaves `path` empty for a title the source tracks without a
	// file; the sub line then carries the badge instead of a filename.
	let titleOnly = $derived(isTitleOnly({ source_path: entry.path }));
	let migration = $derived(entry.migration);
	let monitoredLabel = $derived(
		migration?.monitored
			? i18n.imports_monitored()
			: i18n.imports_not_monitored(),
	);
	let seriesType = $derived(seriesTypeLabel(migration?.seriesType));
	const TONE: Record<string, string> = {
		need: "text-status-wanted",
		ok: "text-status-available",
		link: "text-status-grabbing",
		muted: "text-fg-subtle",
		fail: "text-status-failed",
	};
</script>

<button
	type="button"
	onclick={() => onOpen(entry)}
	class="flex w-full items-center gap-3 px-3.5 py-2.5 text-left transition active:bg-bg-card md:px-4"
>
	<span
		class="h-2 w-2 shrink-0 rounded-full"
		style:background-color="var(--status-{entry.classification === 'confirmed'
			? 'available'
			: entry.classification === 'ambiguous'
				? 'wanted'
				: entry.classification === 'existing'
					? 'grabbing'
					: 'paused'})"
		aria-hidden="true"
	></span>

	<span class="min-w-0 flex-1">
		<span
			class={cn(
				"block truncate text-[13.5px] font-semibold tracking-[-0.01em]",
				entry.headingWeak ? "font-mono text-[12.5px] text-fg-muted" : "text-fg",
			)}
		>
			{entry.heading}
		</span>
		<span
			class="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11px] text-fg-subtle"
		>
			{#if titleOnly}
				<span
					class="shrink-0 rounded-sm border border-status-wanted/30 bg-status-wanted/10 px-1 py-px text-[10px] font-medium uppercase tracking-wide text-status-wanted"
					title={migratedFrom
						? i18n.imports_title_only_help({ app: appLabel(migratedFrom) })
						: undefined}
				>
					{i18n.imports_title_only()}
				</span>
			{:else}
				<span class="min-w-0 truncate font-mono">{#if entry.flag}<span class="font-sans font-medium text-accent-text">{entry.flag}</span>{" · "}{/if}{entry.sub}</span>
			{/if}
			{#if migration}
				<span
					role="img"
					aria-label={monitoredLabel}
					class={cn(
						"inline-flex shrink-0",
						migration.monitored ? "text-fg-muted" : "text-fg-faint",
					)}
				>
					{#if migration.monitored}
						<Eye size={12} aria-hidden="true" />
					{:else}
						<EyeOff size={12} aria-hidden="true" />
					{/if}
				</span>
				{#if migration.qualityProfile}
					<span class="min-w-0 truncate text-fg-muted">
						{migration.qualityProfile}
					</span>
				{/if}
				{#if seriesType}
					<span aria-hidden="true" class="shrink-0 text-fg-faint">·</span>
					<span class="shrink-0 text-fg-muted">{seriesType}</span>
				{/if}
			{/if}
		</span>
	</span>

	{#if wide}
		<span
			class="hidden shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold md:inline-flex"
			style:color="var(--status-{entry.classification === 'confirmed'
				? 'available'
				: entry.classification === 'ambiguous'
					? 'wanted'
					: entry.classification === 'existing'
						? 'grabbing'
						: 'paused'})"
			style:background-color="color-mix(in srgb, var(--status-{entry.classification ===
			'confirmed'
				? 'available'
				: entry.classification === 'ambiguous'
					? 'wanted'
					: entry.classification === 'existing'
						? 'grabbing'
						: 'paused'}) 14%, transparent)"
		>
			{entry.classification.charAt(0).toUpperCase() +
				entry.classification.slice(1)}
		</span>
	{/if}

	<span
		class={cn(
			"shrink-0 text-[12px] font-medium whitespace-nowrap",
			TONE[word.tone],
		)}
	>
		{word.text}
	</span>
	<ChevronRight size={14} class="shrink-0 text-fg-faint" aria-hidden="true" />
</button>
