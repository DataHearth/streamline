<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import { entryHeading } from "../../lib/activity-touch";
	import { pillStatus, formatBytes, formatSpeed, formatEta } from "../../lib/format";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type { QueueEntry, HistoryEntry } from "../../lib/types";

	let {
		item,
		view,
		expanded,
		onToggle,
	}: {
		item: QueueEntry | HistoryEntry;
		view: "queue" | "history";
		expanded: boolean;
		onToggle: (id: number) => void;
	} = $props();

	const pad = "py-3";
	// Data columns hold their content width; the title column takes the remainder
	// and truncates (w-full + max-w-0), so a long release can't widen the table.
	const tight = "w-px whitespace-nowrap";
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

	// Shared with the touch row, which puts the same heading over the release line.
	let heading = $derived(entryHeading(item));
</script>

<tr
	class={cn(
		"group cursor-pointer border-b border-border text-sm transition hover:bg-surface",
		expanded && "bg-bg-card",
	)}
	aria-expanded={expanded}
	onclick={() => onToggle(item.id)}
>
	<td class={cn("pl-4 pr-2", tight, pad)}>
		<StatusPill
			status={pillStatus(item.status)}
			size="sm"
			live={isActive}
		/>
	</td>

	<td class={cn("w-full max-w-0 px-2", pad)}>
		<div class="truncate font-medium text-fg">{heading}</div>
		<div class="truncate font-mono text-[11px] text-fg-subtle">
			{item.title}
		</div>
	</td>

	{#if view === "queue"}
		<td class={cn("px-2", tight, pad)}>
			<div class="flex items-center gap-2">
				<div class="w-24 sm:w-28">
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
		<td class={cn("px-2 tabular-nums text-xs text-fg-muted", tight, pad)}>
			{speedEta || "—"}
		</td>
		<td class={cn("hidden px-2 text-xs text-fg-subtle @3xl:table-cell", tight, pad)}>
			{queue.download_client || "—"}
		</td>
	{:else}
		<td class={cn("hidden px-2 text-xs text-fg-subtle @3xl:table-cell", tight, pad)}>
			{history.indexer || "—"}
		</td>
		<td class={cn("px-2 tabular-nums text-xs text-fg-muted", tight, pad)}>
			{formatBytes(history.size)}
		</td>
		<td
			class={cn("px-2 text-xs text-fg-subtle", tight, pad)}
			title={formatDateTime(history.updated_at)}
		>
			{formatRelative(history.updated_at)}
		</td>
	{/if}

	<td class={cn("pr-4 pl-2 text-right", tight, pad)}>
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
