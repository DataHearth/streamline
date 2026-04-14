<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import { pillStatus, formatBytes, formatSpeed, formatEta } from "../../lib/format";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type { QueueEntry, HistoryEntry } from "../../lib/types";

	let {
		item,
		view,
		density,
		expanded,
		onToggle,
	}: {
		item: QueueEntry | HistoryEntry;
		view: "queue" | "history";
		density: "comfortable" | "compact";
		expanded: boolean;
		onToggle: (id: number) => void;
	} = $props();

	let pad = $derived(density === "comfortable" ? "py-3" : "py-2");
	let isActive = $derived(view === "queue" && item.status === "downloading");
	let queue = $derived(item as QueueEntry);
	let history = $derived(item as HistoryEntry);

	let speedEta = $derived.by(() => {
		const parts = [
			formatSpeed(queue.download_speed),
			formatEta(queue.eta),
		].filter(Boolean);
		return parts.join(" · ");
	});

	// Episode records carry no movie; render "<show> · SxxExx" instead.
	function pad2(n: number): string {
		return String(n).padStart(2, "0");
	}
	const episodeTokenRe = /S\d{1,2}E\d{1,2}/i;
	let heading = $derived.by(() => {
		const ep = item.episode;
		if (!ep) return item.movie.title;
		// A season pack names no specific episode, so its linked episode is just
		// the season's first — label it as the season, not a misleading "SxxE01".
		if (episodeTokenRe.test(item.title)) {
			return `${ep.show_title} · S${pad2(ep.season)}E${pad2(ep.episode)}`;
		}
		const season = ep.season === 0 ? "Specials" : `Season ${pad2(ep.season)}`;
		return `${ep.show_title} · ${season}`;
	});
</script>

<tr
	class={cn(
		"group cursor-pointer border-b border-border text-sm transition hover:bg-surface",
		expanded && "bg-bg-card",
	)}
	aria-expanded={expanded}
	onclick={() => onToggle(item.id)}
>
	<td class={cn("pl-4 pr-2", pad)}>
		<StatusPill
			status={pillStatus(item.status)}
			size="sm"
			live={isActive}
		/>
	</td>

	<td class={cn("min-w-0 px-2", pad)}>
		<div class="truncate font-medium text-fg">{heading}</div>
		<div class="truncate font-mono text-[11px] text-fg-subtle">
			{item.title}
		</div>
	</td>

	{#if view === "queue"}
		<td class={cn("px-2", pad)}>
			<div class="flex items-center gap-2">
				<div class="w-24 sm:w-32">
					<ProgressBar
						value={queue.status === "importing" ? 1 : queue.progress}
						status={queue.status === "importing"
							? "grabbing"
							: "downloading"}
						height={4}
						shimmer={isActive}
					/>
				</div>
				<span class="tabular-nums text-xs text-fg-muted">
					{Math.round((queue.progress ?? 0) * 100)}%
				</span>
			</div>
		</td>
		<td class={cn("px-2 tabular-nums text-xs text-fg-muted", pad)}>
			{speedEta || "—"}
		</td>
		<td class={cn("px-2 text-xs text-fg-subtle", pad)}>
			{queue.download_client || "—"}
		</td>
	{:else}
		<td class={cn("px-2 text-xs text-fg-subtle", pad)}>
			{history.indexer || "—"}
		</td>
		<td class={cn("px-2 tabular-nums text-xs text-fg-muted", pad)}>
			{formatBytes(history.size)}
		</td>
		<td
			class={cn("px-2 text-xs text-fg-subtle", pad)}
			title={formatDateTime(history.updated_at)}
		>
			{formatRelative(history.updated_at)}
		</td>
	{/if}

	<td class={cn("pr-4 pl-2 text-right", pad)}>
		<button
			type="button"
			aria-label={expanded ? "Collapse details" : "Expand details"}
			aria-expanded={expanded}
			onclick={(e) => {
				e.stopPropagation();
				onToggle(item.id);
			}}
			class="inline-flex h-7 w-7 items-center justify-center rounded-md text-fg-faint transition hover:bg-bg-subtle hover:text-fg"
		>
			<ChevronRight
				size={15}
				class={cn(
					"transition-transform motion-safe:duration-200",
					expanded && "rotate-90",
				)}
				aria-hidden="true"
			/>
		</button>
	</td>
</tr>
