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
	import {
		audioLabel,
		audioTracks,
		channelLayout,
		codecLabel,
		formatBitrate,
		formatDuration,
		langName,
		probeOf,
		resolutionBucket,
		subtitleFlags,
		subtitleTracks,
	} from "../../lib/media-info";
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

	let probe = $derived(episode ? probeOf(episode) : null);
	let audioList = $derived(audioTracks(probe));
	let subList = $derived(subtitleTracks(probe));
	// Only when the streams could not be enumerated: otherwise the track list
	// below says the same thing per language.
	let flatAudio = $derived(audioList.length === 0 ? audioLabel(probe) : undefined);

	// Aliased for the same reason as MovieDetailInfo's TrackToggle: a snippet's
	// parameter list cannot carry an inline object type.
	type TrackListRow = {
		name: string;
		note?: string;
		flags: string[];
		value: string;
	};
	type TrackListRows = TrackListRow[];
	// Only rows the probe actually carries: an audio-less remux has no Audio row
	// rather than a dash.
	let probeRows = $derived(
		probe
			? [
					{ label: i18n.file_container(), value: probe.container?.toUpperCase() },
					{ label: i18n.file_video(), value: codecLabel(probe.video_codec) },
					{
						label: i18n.file_resolution(),
						value: resolutionBucket(probe.width, probe.height),
					},
					{ label: i18n.file_duration(), value: formatDuration(probe.duration_seconds) },
					{ label: i18n.file_audio(), value: flatAudio },
					{ label: i18n.file_bitrate(), value: formatBitrate(probe.bitrate) },
				].filter((r) => Boolean(r.value))
			: [],
	);

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

{#snippet trackList(
	heading: string,
	rows: TrackListRows,
)}
	<div class="mt-3.5">
		<h5
			class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			{heading}
		</h5>
		<div class="mt-1 divide-y divide-border border-y border-border">
			{#each rows as row, k (k)}
				<div class="flex items-baseline justify-between gap-3 py-1.5 text-sm">
					<span class="min-w-0 truncate text-fg-muted">
						{row.name}
						{#if row.note}
							<span class="text-fg-faint">· {row.note}</span>
						{/if}
						{#each row.flags as flag (flag)}
							<span
								class="ml-1 text-[10px] uppercase tracking-[0.1em] text-fg-faint"
							>
								{flag}
							</span>
						{/each}
					</span>
					<span class="shrink-0 font-mono text-xs text-fg-subtle">
						{row.value}
					</span>
				</div>
			{/each}
		</div>
	</div>
{/snippet}

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
			{#if episode.file_score !== undefined}
				<span
					class={cn(
						"shrink-0 rounded-full border px-1.5 py-px font-mono text-[9px] font-semibold uppercase tracking-[0.1em]",
						episode.file_score > 0
							? "border-status-available/30 bg-status-available/10 text-status-available"
							: episode.file_score < 0
								? "border-status-failed/30 bg-status-failed/10 text-status-failed"
								: "border-border-strong text-fg-subtle",
					)}
					title={i18n.file_score_help()}
				>
					{i18n.file_score()}
					{episode.file_score}
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

		{#if probeRows.length > 0}
			<div class="mt-4 border-t border-border pt-3">
				<div class="flex items-center gap-2">
					<h4
						class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
					>
						{i18n.file_media()}
					</h4>
					<span
						class="rounded-full border border-status-available/30 bg-status-available/10 px-1.5 py-px font-mono text-[9px] font-semibold uppercase tracking-[0.1em] text-status-available"
					>
						{i18n.file_probed()}
					</span>
				</div>
				<dl class="mt-2.5 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
					{#each probeRows as row (row.label)}
						<dt class="text-fg-subtle">{row.label}</dt>
						<dd class="text-right font-mono text-xs tabular text-fg-muted">
							{row.value}
						</dd>
					{/each}
				</dl>

				{#if audioList.length > 0}
					{@render trackList(
						i18n.file_audio_tracks({ count: audioList.length }),
						audioList.map((t) => ({
							name: langName(t.language),
							note: t.title,
							flags: t.default ? [i18n.track_default()] : [],
							value:
								[codecLabel(t.codec), channelLayout(t.channels)]
									.filter(Boolean)
									.join(" · ") || "—",
						})),
					)}
				{/if}
				{#if subList.length > 0}
					{@render trackList(
						i18n.file_subtitle_tracks({ count: subList.length }),
						subList.map((t) => ({
							name: langName(t.language),
							note: undefined,
							flags: subtitleFlags(t),
							value: codecLabel(t.codec) ?? "—",
						})),
					)}
				{/if}
			</div>
		{/if}

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
