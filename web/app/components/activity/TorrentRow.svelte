<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import ProgressBar from "@components/shared/ProgressBar.svelte";
	import SelectBox from "@components/shared/SelectBox.svelte";
	import { cn } from "@lib/cn";
	import {
		formatBytes,
		formatSpeed,
		formatEta,
		formatRatio,
	} from "@lib/format";
	import { torrentRelease } from "@lib/activity-touch";
	import type { Torrent } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		torrent,
		selected = false,
		checked,
		onCheck,
		onOpen,
	}: {
		torrent: Torrent;
		// The drawer is open on this row.
		selected?: boolean;
		// Bulk selection; undefined when the table has no checkbox column.
		checked?: boolean;
		onCheck?: (hash: string, on: boolean) => void;
		onOpen: (hash: string) => void;
	} = $props();

	// w-px keeps a data column at its content width; the name column (w-full
	// max-w-0) then absorbs everything left over instead of overflowing.
	const pad = "w-px whitespace-nowrap py-3";
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
		checked && "bg-accent-soft",
	)}
	onclick={() => onOpen(torrent.hash)}
>
	{#if checked !== undefined}
		<td class={cn("pl-4 pr-1", pad)}>
			<SelectBox
				{checked}
				onChange={(v) => onCheck?.(torrent.hash, v)}
				label={checked
					? i18n.a11y_deselect_item({ title: torrent.name || torrent.hash })
					: i18n.a11y_select_item({ title: torrent.name || torrent.hash })}
			/>
		</td>
	{/if}
	<td class={cn(checked === undefined ? "pl-4 pr-2" : "px-2", pad)}>
		<StatusPill status={torrent.status} size="sm" live={active} />
	</td>

	<!-- w-full + max-w-0: the name column soaks up whatever width is left and
	     truncates, rather than widening the table past its container. -->
	<td class="w-full max-w-0 px-2 py-3">
		{#if fetching && !torrent.name}
			<div class="flex items-center gap-2">
				<span class="italic text-fg-muted">{i18n.torrent_fetching_metadata()}</span>
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
						title={i18n.torrent_not_linked()}
					>
						{i18n.torrent_untracked()}
					</span>
				{/if}
			</div>
			{@const release = torrentRelease(torrent)}
			{#if release}
				<div class="truncate text-[11px] text-fg-subtle" title={release}>
					{release}
				</div>
			{:else}
				<div class="truncate font-mono text-[11px] text-fg-subtle">
					{torrent.hash.slice(0, 24)}…
				</div>
			{/if}
		{/if}
	</td>

	<td class={cn("px-2", pad)}>
		<div class="flex items-center gap-2">
			<div class="w-14 sm:w-20">
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

	<!-- Narrow containers get one stacked cell instead of two columns; the
	     separate Down and Up columns come back at @3xl. -->
	<td class={cn("px-2 leading-tight @3xl:hidden", num, pad)}>
		<div class={torrent.download_speed > 0 ? "text-status-downloading" : "text-fg-faint"}>
			{formatSpeed(torrent.download_speed) || "—"}
		</div>
		<div class={torrent.upload_speed > 0 ? "text-status-seeding" : "text-fg-faint"}>
			{formatSpeed(torrent.upload_speed) || "—"}
		</div>
	</td>

	<td class={cn("hidden px-2 @3xl:table-cell", num, pad)}>
		<span class={torrent.download_speed > 0 ? "text-status-downloading" : "text-fg-faint"}>
			{formatSpeed(torrent.download_speed) || "—"}
		</span>
	</td>
	<td class={cn("hidden px-2 @3xl:table-cell", num, pad)}>
		<span class={torrent.upload_speed > 0 ? "text-status-seeding" : "text-fg-faint"}>
			{formatSpeed(torrent.upload_speed) || "—"}
		</span>
	</td>
	<td class={cn("hidden px-2 text-fg-muted @3xl:table-cell", num, pad)}>
		{formatBytes(torrent.size)}
	</td>
	<td class={cn("hidden px-2 text-fg-muted @4xl:table-cell", num, pad)}>
		{fetching ? "—" : formatRatio(torrent.ratio)}
	</td>
	<td class={cn("hidden px-2 text-fg-muted @4xl:table-cell", num, pad)}>
		<!-- seeds/peers are connections, so an idle swarm and an unreachable
		     engine both read 0/0. known_peers is what separates them, shown as
		     a third figure only when there is nobody connected to talk about. -->
		{#if torrent.peer_count === 0 && torrent.seeds === 0}
			{#if torrent.known_peers}
				<span class="text-fg-faint" title={i18n.torrent_known_peers_help()}>
					{i18n.torrent_known_peers({ count: torrent.known_peers })}
				</span>
			{:else}
				—
			{/if}
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
