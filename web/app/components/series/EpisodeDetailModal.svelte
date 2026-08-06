<script lang="ts" module>
	import type { EpisodeDisplayStatus } from "../../lib/status";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Episode statuses map onto the shared status color tokens. "unaired" and
	// "skipped" have no dedicated status pill, so they borrow neutral tones.
	// Exported because the desktop table's rows render the same pill.
	export const STATUS_META: Record<
		EpisodeDisplayStatus,
		{ label: string; token: string; live?: boolean }
	> = {
		available: { label: i18n.status_available(), token: "available" },
		wanted: { label: i18n.status_wanted(), token: "wanted" },
		missing: { label: i18n.status_missing(), token: "missing" },
		downloading: { label: i18n.status_downloading(), token: "downloading", live: true },
		paused: { label: i18n.status_paused(), token: "paused" },
		unaired: { label: i18n.series_unaired(), token: "missing" },
		skipped: { label: i18n.common_skipped(), token: "paused" },
	};
</script>

<script lang="ts">
	import { Search, Trash2 } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { episodeStatus } from "../../lib/status";
	import Modal from "../modals/Modal.svelte";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import { formatBytes } from "../../lib/format";
	import type { Episode } from "../../lib/types";

	// Lifted out of EpisodeTable so the phone accordion opens the same sheet the
	// desktop table does — the two can't drift into two ideas of "episode info".
	let {
		episode,
		code,
		onClose,
		onManualSearch,
		onDeleteFile,
	}: {
		episode: Episode | null;
		code: string;
		onClose: () => void;
		onManualSearch: (ep: Episode) => void;
		onDeleteFile: (ep: Episode) => void;
	} = $props();

	let rows = $derived(
		episode
			? [
					{ label: i18n.common_episode(), value: code },
					{
						label: i18n.series_absolute_num(),
						value: episode.absolute_number ? `#${episode.absolute_number}` : "—",
					},
					{ label: i18n.series_air_date(), value: formatDateTime(episode.air_date) || "—" },
					{ label: i18n.monitor_monitored(), value: episode.monitored ? i18n.common_yes() : i18n.common_no() },
					{ label: i18n.common_quality(), value: episode.quality || "—" },
					{ label: i18n.common_size(), value: formatBytes(episode.size) },
				]
			: [],
	);
</script>

{#if episode}
	{@const meta = STATUS_META[episodeStatus(episode)]}
	<Modal open={true} title={episode.title || "TBA"} size="xl" {onClose}>
		<div class="flex flex-wrap items-center gap-2">
			<span
				class="ep-pill inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[10.5px] font-semibold"
				style:--c={`var(--status-${meta.token})`}
			>
				<span
					class={cn(
						"dot h-1.5 w-1.5 shrink-0 rounded-full",
						meta.live && "motion-safe:animate-pulse",
					)}
				></span>
				{meta.label}
			</span>
			{#if episode.air_date}
				<span class="text-xs text-fg-subtle">
					{formatRelative(episode.air_date)}
				</span>
			{/if}
		</div>

		{#if episode.overview}
			<p class="mt-3 text-sm leading-relaxed text-fg-muted">{episode.overview}</p>
		{/if}

		<dl class="mt-4 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-sm">
			{#each rows as row (row.label)}
				<dt class="text-fg-subtle">{row.label}</dt>
				<dd class="text-right font-mono text-xs tabular text-fg-muted">
					{row.value}
				</dd>
			{/each}
		</dl>

		{#if episode.path}
			<dl class="mt-4 border-t border-border pt-3">
				<dt class="text-sm text-fg-subtle">{i18n.common_file()}</dt>
				<dd
					class="mt-1 break-all font-mono text-xs text-fg-muted"
					title={episode.path}
				>
					{episode.path}
				</dd>
			</dl>
		{/if}

		{#snippet footer()}
			{#if (episode?.size ?? 0) > 0}
				<button
					type="button"
					onclick={() => {
						const ep = episode;
						onClose();
						if (ep) onDeleteFile(ep);
					}}
					class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3.5 text-sm font-medium text-fg-muted transition hover:border-status-failed/40 hover:bg-status-failed/10 hover:text-status-failed"
				>
					<Trash2 size={15} aria-hidden="true" />
					{i18n.action_delete_file()}
				</button>
			{/if}
			{#if episode && episode.status !== "unaired"}
				<button
					type="button"
					onclick={() => {
						const ep = episode;
						onClose();
						if (ep) onManualSearch(ep);
					}}
					class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-medium text-fg-on-accent transition hover:bg-accent-hover"
				>
					<Search size={15} aria-hidden="true" />
					{i18n.action_manual_search()}
				</button>
			{/if}
		{/snippet}
	</Modal>
{/if}

<style>
	.ep-pill {
		background-color: color-mix(in srgb, var(--c) 15%, transparent);
		color: var(--c);
	}
	.ep-pill .dot {
		background-color: var(--c);
	}
</style>
