<script lang="ts">
	import { ChevronDown, ExternalLink, Trash2 } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { auth } from "../../lib/auth.svelte";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import { cn } from "../../lib/cn";
	import {
		audioSummary,
		audioTracks,
		channelLayout,
		codecLabel,
		codecOf,
		formatBitrate,
		formatDuration,
		langName,
		probeOf,
		resolutionOf,
		subtitleFlags,
		subtitleSummary,
		subtitleTracks,
		type TrackSummary,
	} from "../../lib/media-info";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { Movie } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		movie,
		qualityProfileName,
	}: {
		movie: Movie;
		qualityProfileName: string | null;
	} = $props();

	let primary = $derived(movie.media_files?.[0]);
	// The panel groups by provenance rather than marking values one by one: what
	// ffprobe read, what the release name claimed, and what is on disk. An
	// unprobed file is the same panel without its first group — no empty rows and
	// no placeholder dashes, so it renders exactly what it rendered before.
	let probe = $derived(primary ? probeOf(primary) : null);

	// The panel states what tracks exist; the full per-track list expands in
	// place, because on a movie this aside IS the detail surface. A single track
	// carries its codec inline and has nothing to expand to.
	let audio = $derived(audioSummary(probe));
	let subs = $derived(subtitleSummary(probe));
	let audioList = $derived(audioTracks(probe));
	let subList = $derived(subtitleTracks(probe));
	let audioOpen = $state(false);
	let subsOpen = $state(false);

	// Aliased because a snippet's parameter list cannot carry the type inline:
	// Svelte's snippet parser takes simple annotations only, and an arrow-function
	// type there is a compile error.
	type TrackToggle = () => void;

	// The path is far wider than the column and truncates. Desktop has the
	// title tooltip; touch has neither hover nor room, so a press and hold
	// puts the whole path on the clipboard — the same 480ms/8px gesture the
	// poster grid uses for selection.
	const HOLD_MS = 480;
	const HOLD_SLOP = 8;
	let holdTimer: number | null = null;
	let holdX = 0;
	let holdY = 0;
	let holding = $state(false);
	let longPressed = false;
	let touchGesture = false;

	async function copyPath() {
		if (!primary?.path) return;
		try {
			await navigator.clipboard.writeText(primary.path);
			toast.ok("Path copied");
		} catch {
			toast.err("Clipboard unavailable");
		}
	}
	function cancelHold() {
		holding = false;
		if (holdTimer !== null) {
			clearTimeout(holdTimer);
			holdTimer = null;
		}
	}
	function onPointerDown(e: PointerEvent) {
		longPressed = false;
		touchGesture = e.pointerType !== "mouse";
		if (!touchGesture) return;
		cancelHold();
		holdX = e.clientX;
		holdY = e.clientY;
		holding = true;
		holdTimer = window.setTimeout(() => {
			holdTimer = null;
			holding = false;
			longPressed = true;
			navigator.vibrate?.(10);
			copyPath();
		}, HOLD_MS);
	}
	function onPointerMove(e: PointerEvent) {
		if (holdTimer === null) return;
		if (
			Math.abs(e.clientX - holdX) > HOLD_SLOP ||
			Math.abs(e.clientY - holdY) > HOLD_SLOP
		)
			cancelHold();
	}
	// A tap is not a copy — touch waits for the hold. Keyboard activation
	// (detail 0) always copies.
	function onClick(e: MouseEvent) {
		if (e.detail === 0 || !touchGesture) copyPath();
	}
	function onContextMenu(e: MouseEvent) {
		// Android raises its own menu on the same gesture.
		if (longPressed || holding) e.preventDefault();
	}

	const qc = useQueryClient();
	let deleteOpen = $state(false);
	let removeTorrent = $state(false);

	const del = createMutation<unknown, Error, { fileId: number; remove: boolean }>(
		() => ({
			mutationFn: ({ fileId, remove }) =>
				api(`/movies/${movie.id}/files/${fileId}`, {
					method: "DELETE",
					body: { remove_torrent: remove },
				}),
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["movie", movie.id] });
				toast.ok("File deleted");
				deleteOpen = false;
			},
			onError: (e) => toast.err(errorText(e, i18n.common_delete_failed())),
		}),
	);
</script>

<aside class="flex flex-col gap-4 md:row-span-2">
	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-file"
	>
		<div class="flex items-center justify-between gap-2">
			<div class="flex min-w-0 items-center gap-2">
				<h4
					id="info-file"
					class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
				>
					{i18n.common_file()}
				</h4>
				{#if primary}
					<span
						class={cn(
							"shrink-0 rounded-full border px-1.5 py-px font-mono text-[9px] font-semibold uppercase tracking-[0.1em]",
							probe
								? "border-status-available/30 bg-status-available/10 text-status-available"
								: "border-border-strong text-fg-subtle",
						)}
					>
						{probe ? i18n.file_probed() : i18n.file_not_probed()}
					</span>
				{/if}
				{#if primary?.file_score !== undefined}
					<span
						class={cn(
							"shrink-0 rounded-full border px-1.5 py-px font-mono text-[9px] font-semibold uppercase tracking-[0.1em]",
							primary.file_score > 0
								? "border-status-available/30 bg-status-available/10 text-status-available"
								: primary.file_score < 0
									? "border-status-failed/30 bg-status-failed/10 text-status-failed"
									: "border-border-strong text-fg-subtle",
						)}
						title={i18n.file_score_help()}
					>
						{i18n.file_score()}
						{primary.file_score}
					</span>
				{/if}
			</div>
			{#if primary && auth.canAddDirectly}
				<button
					type="button"
					onclick={() => {
						removeTorrent = false;
						deleteOpen = true;
					}}
					aria-label={i18n.action_delete_file()}
					title={i18n.action_delete_file()}
					class="grid h-7 w-7 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
				>
					<Trash2 class="h-4 w-4" aria-hidden="true" />
				</button>
			{/if}
		</div>
		{#if primary}
			<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
				{#if probe}
					{#if probe.container}
						<dt class="text-fg-subtle">{i18n.file_container()}</dt>
						<dd class="text-right font-mono text-fg">
							{probe.container.toUpperCase()}
						</dd>
					{/if}
					{#if codecOf(primary)}
						<dt class="text-fg-subtle">{i18n.file_video()}</dt>
						<dd class="text-right font-mono text-fg">{codecOf(primary)}</dd>
					{/if}
					{#if resolutionOf(primary)}
						<dt class="text-fg-subtle">{i18n.file_resolution()}</dt>
						<dd class="text-right font-mono text-fg">{resolutionOf(primary)}</dd>
					{/if}
					{#if formatDuration(probe.duration_seconds)}
						<dt class="text-fg-subtle">{i18n.file_duration()}</dt>
						<dd class="text-right font-mono text-fg">
							{formatDuration(probe.duration_seconds)}
						</dd>
					{/if}
					{#if audio}
						{@render trackRow(
							i18n.file_audio(),
							audio,
							audioList.length > 1,
							audioOpen,
							() => (audioOpen = !audioOpen),
						)}
						{#if audioOpen}
							<div class="col-span-2 mt-0.5 divide-y divide-border border-y border-border">
								{#each audioList as t, k (k)}
									<div class="flex items-baseline justify-between gap-3 py-1.5">
										<span class="min-w-0 truncate text-fg-muted">
											{langName(t.language)}
											{#if t.title}
												<span class="text-fg-faint">· {t.title}</span>
											{/if}
											{#if t.default}
												<span class="text-[10px] uppercase tracking-[0.1em] text-accent-text">
													{i18n.track_default()}
												</span>
											{/if}
										</span>
										<span class="shrink-0 font-mono text-fg-subtle">
											{[codecLabel(t.codec), channelLayout(t.channels)]
												.filter(Boolean)
												.join(" · ")}
										</span>
									</div>
								{/each}
							</div>
						{/if}
					{/if}
					{#if subs}
						{@render trackRow(
							i18n.file_subtitles(),
							subs,
							subList.length > 1,
							subsOpen,
							() => (subsOpen = !subsOpen),
						)}
						{#if subsOpen}
							<div class="col-span-2 mt-0.5 divide-y divide-border border-y border-border">
								{#each subList as t, k (k)}
									<div class="flex items-baseline justify-between gap-3 py-1.5">
										<span class="min-w-0 truncate text-fg-muted">
											{langName(t.language)}
											{#each subtitleFlags(t) as flag (flag)}
												<span class="text-[10px] uppercase tracking-[0.1em] text-fg-faint">
													{flag}
												</span>
											{/each}
										</span>
										<span class="shrink-0 font-mono text-fg-subtle">
											{codecLabel(t.codec) ?? "—"}
										</span>
									</div>
								{/each}
							</div>
						{/if}
					{/if}
					{#if formatBitrate(probe.bitrate)}
						<dt class="text-fg-subtle">{i18n.file_bitrate()}</dt>
						<dd class="text-right font-mono text-fg">
							{formatBitrate(probe.bitrate)}
						</dd>
					{/if}
				{/if}
				{@render group(i18n.file_from_release())}
				{#if !probe}
					<dt class="text-fg-subtle">{i18n.file_resolution()}</dt>
					<dd class="text-right font-mono text-fg">
						{primary.parsed_resolution || "—"}
					</dd>
					<dt class="text-fg-subtle">{i18n.file_codec()}</dt>
					<dd class="text-right font-mono text-fg">
						{primary.parsed_codec || "—"}
					</dd>
				{/if}
				<dt class="text-fg-subtle">{i18n.file_source()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_source || "—"}
				</dd>
				<dt class="text-fg-subtle">{i18n.file_group()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.release_group || "—"}
				</dd>
				{@render group(i18n.file_on_disk())}
				<dt class="text-fg-subtle">{i18n.common_size()}</dt>
				<dd class="text-right font-mono text-fg">
					{formatBytes(primary.size)}
				</dd>
				<dt class="text-fg-subtle">{i18n.field_path()}</dt>
				<dd class="min-w-0">
					<button
						type="button"
						onpointerdown={onPointerDown}
						onpointermove={onPointerMove}
						onpointerup={cancelHold}
						onpointercancel={cancelHold}
						oncontextmenu={onContextMenu}
						onclick={onClick}
						title={primary.path}
						aria-label={i18n.action_copy_file_path()}
						class="block w-full truncate rounded text-right font-mono underline decoration-border-strong decoration-dotted underline-offset-[3px] transition-colors [-webkit-touch-callout:none] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {holding
							? 'text-accent-text'
							: 'text-fg'}"
					>
						{primary.path}
					</button>
				</dd>
			</dl>
		{:else}
			<p class="mt-3 text-[12px] text-fg-subtle">
				No file yet.{movie.status === "wanted"
					? " Searching nightly."
					: ""}
			</p>
		{/if}
	</section>

	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-library"
	>
		<h4
			id="info-library"
			class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
		>
			{i18n.nav_library()}
		</h4>
		<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
			<dt class="text-fg-subtle">{i18n.quality_profile()}</dt>
			<dd class="text-right font-mono text-fg">
				{qualityProfileName ?? "—"}
			</dd>
			<dt class="text-fg-subtle">{i18n.common_status()}</dt>
			<dd class="text-right font-mono text-fg capitalize">
				{movie.status}
			</dd>
			<dt class="text-fg-subtle">{i18n.monitor_monitored()}</dt>
			<dd class="text-right font-mono text-fg">
				{movie.monitored ? i18n.common_yes() : i18n.common_no()}
			</dd>
			{#if movie.year}
				<dt class="text-fg-subtle">{i18n.common_year()}</dt>
				<dd class="text-right font-mono text-fg">{movie.year}</dd>
			{/if}
			{#if movie.runtime}
				<dt class="text-fg-subtle">{i18n.detail_runtime()}</dt>
				<dd class="text-right font-mono text-fg">{movie.runtime}m</dd>
			{/if}
			<dt class="text-fg-subtle">TMDB</dt>
			<dd class="text-right">
				<a
					href="https://www.themoviedb.org/movie/{movie.tmdb_id}"
					target="_blank"
					rel="noopener noreferrer"
					class="inline-flex items-center gap-1 font-mono text-accent-text transition hover:text-accent"
				>
					{movie.tmdb_id}
					<ExternalLink size={11} aria-hidden="true" />
				</a>
			</dd>
		</dl>
	</section>
</aside>

{#snippet trackRow(
	label: string,
	sum: TrackSummary,
	expandable: boolean,
	open: boolean,
	toggle: TrackToggle,
)}
	<dt class="text-fg-subtle">{label}</dt>
	<dd class="min-w-0 text-right">
		{#if expandable}
			<button
				type="button"
				onclick={toggle}
				aria-expanded={open}
				class="inline-flex max-w-full items-center justify-end gap-1.5 rounded font-mono text-fg transition-colors hover:text-accent-text focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				<span class="truncate">{sum.text}</span>
				{#if sum.hidden > 0}
					<span class="shrink-0 text-accent-text">+{sum.hidden}</span>
				{/if}
				<ChevronDown
					size={12}
					class="shrink-0 text-fg-faint transition-transform motion-safe:duration-200 {open
						? 'rotate-180'
						: ''}"
					aria-hidden="true"
				/>
			</button>
		{:else}
			<span class="font-mono text-fg">{sum.text}</span>
		{/if}
	</dd>
{/snippet}

{#snippet group(label: string)}
	<div
		class="col-span-2 mt-2 flex items-center gap-2.5 font-mono text-[9px] uppercase tracking-[0.12em] text-fg-faint"
	>
		<span>{label}</span>
		<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
	</div>
{/snippet}

<Dialog
	open={deleteOpen}
	title={i18n.file_delete_confirm()}
	onClose={() => (deleteOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.action_delete_file(),
			variant: "danger",
			dismiss: false,
			pending: del.isPending,
			onClick: () =>
				primary && del.mutate({ fileId: primary.id, remove: removeTorrent }),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{i18n.file_removed_reverts()} <span
			class="font-medium text-fg">wanted</span
		>, so the next monitored search re-grabs it.
	</p>
	<Checkbox
		checked={removeTorrent}
		onChange={(v) => (removeTorrent = v)}
		class="mt-4 text-sm text-fg"
	>
		{i18n.file_also_remove_torrent()}
	</Checkbox>
</Dialog>
