<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import {
		formatBytes,
		formatSpeed,
		formatEta,
		formatRatio,
	} from "../../lib/format";
	import type { Torrent } from "../../lib/types";

	let {
		torrent,
		density,
		selected = false,
		onOpen,
	}: {
		torrent: Torrent;
		density: "comfortable" | "compact";
		selected?: boolean;
		onOpen: (hash: string) => void;
	} = $props();

	let pad = $derived(density === "comfortable" ? "py-3" : "py-2");
	let fetching = $derived(torrent.status === "fetching");
	let active = $derived(
		torrent.status === "downloading" ||
			torrent.status === "seeding" ||
			torrent.status === "fetching",
	);
	let num = "font-mono tabular-nums text-xs";
</script>

<tr
	class={cn(
		"group cursor-pointer border-b border-border text-sm transition hover:bg-surface",
		selected && "bg-bg-card",
	)}
	onclick={() => onOpen(torrent.hash)}
>
	<td class={cn("pl-4 pr-2", pad)}>
		<StatusPill status={torrent.status} size="sm" live={active} />
	</td>

	<td class={cn("min-w-0 px-2", pad)}>
		{#if fetching && !torrent.name}
			<div class="flex items-center gap-2">
				<span class="italic text-fg-muted">Fetching metadata…</span>
			</div>
			<div class="truncate font-mono text-[11px] text-fg-faint">
				{torrent.hash.slice(0, 24)}…
			</div>
		{:else}
			<div class="flex items-center gap-1.5">
				<span class="truncate font-medium text-fg">{torrent.name}</span>
				{#if !torrent.tracked}
					<span
						class="shrink-0 rounded-full border border-border px-1.5 py-px text-[9px] font-semibold uppercase tracking-wide text-fg-subtle"
						title="Not linked to a library item"
					>
						untracked
					</span>
				{/if}
			</div>
			<div class="truncate font-mono text-[11px] text-fg-subtle">
				{torrent.hash.slice(0, 24)}…
			</div>
		{/if}
	</td>

	<td class={cn("px-2", pad)}>
		<div class="flex items-center gap-2">
			<div class="w-20 sm:w-28">
				<ProgressBar
					value={fetching ? undefined : torrent.progress}
					status={torrent.status}
					height={4}
					shimmer={torrent.status === "downloading"}
				/>
			</div>
			<div class="flex flex-col leading-tight">
				<span class="font-mono tabular-nums text-xs text-fg-muted">
					{fetching ? "—" : `${Math.round(torrent.progress * 100)}%`}
				</span>
				{#if torrent.status === "downloading" && torrent.eta > 0}
					<span class="font-mono tabular-nums text-[10px] text-fg-faint">
						{formatEta(torrent.eta)}
					</span>
				{/if}
			</div>
		</div>
	</td>

	<td class={cn("px-2 text-fg-muted", num, pad)}>
		{formatBytes(torrent.size)}
	</td>
	<td class={cn("px-2", num, pad)}>
		<span class={torrent.download_speed > 0 ? "text-status-downloading" : "text-fg-faint"}>
			{formatSpeed(torrent.download_speed) || "—"}
		</span>
	</td>
	<td class={cn("px-2", num, pad)}>
		<span class={torrent.upload_speed > 0 ? "text-status-seeding" : "text-fg-faint"}>
			{formatSpeed(torrent.upload_speed) || "—"}
		</span>
	</td>
	<td class={cn("px-2 text-fg-muted", num, pad)}>
		{fetching ? "—" : formatRatio(torrent.ratio)}
	</td>
	<td class={cn("px-2 text-fg-muted", num, pad)}>
		{#if torrent.peer_count === 0 && torrent.seeds === 0}
			—
		{:else}
			<span class="text-fg-muted">{torrent.seeds}</span>
			<span class="text-fg-faint"> / {torrent.peer_count}</span>
		{/if}
	</td>

	<td class={cn("pr-4 pl-2 text-right", pad)}>
		<span
			class="inline-flex h-7 w-7 items-center justify-center rounded-md text-fg-faint transition group-hover:text-fg"
			aria-hidden="true"
		>
			<ChevronRight size={15} />
		</span>
	</td>
</tr>
