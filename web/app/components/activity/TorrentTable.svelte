<script lang="ts">
	import { Magnet, Zap, ArrowUpRight } from "@lucide/svelte";
	import TorrentRow from "./TorrentRow.svelte";
	import type { Torrent } from "../../lib/types";

	let {
		rows,
		density,
		loading,
		error = null,
		notConfigured = false,
		canControl = false,
		selectedHash = null,
		onOpen,
		onAdd,
	}: {
		rows: Torrent[];
		density: "comfortable" | "compact";
		loading: boolean;
		error?: Error | null;
		notConfigured?: boolean;
		canControl?: boolean;
		selectedHash?: string | null;
		onOpen: (hash: string) => void;
		onAdd: () => void;
	} = $props();

	const HEADERS = [
		"Status",
		"Name",
		"Progress",
		"Size",
		"Down",
		"Up",
		"Ratio",
		"Seeds / Peers",
		"",
	];
</script>

{#if notConfigured}
	<!-- /torrents 404s when no built-in client is configured — nudge, not a
	     broken table. -->
	<div
		class="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-5 py-14 text-center"
	>
		<div class="grid h-12 w-12 place-items-center rounded-full bg-accent-soft text-accent">
			<Zap size={22} aria-hidden="true" />
		</div>
		<div>
			<p class="text-sm font-semibold text-fg">
				The built-in client isn’t enabled
			</p>
			<p class="mx-auto mt-1 max-w-sm text-xs text-fg-muted">
				Enable Streamline’s built-in BitTorrent engine to add and manage
				torrents from here.
			</p>
		</div>
		{#if canControl}
			<a
				href="/settings/download-clients"
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
			>
				Enable in Settings
				<ArrowUpRight size={15} aria-hidden="true" />
			</a>
		{:else}
			<p class="text-xs text-fg-subtle">Ask an admin to enable it in Settings.</p>
		{/if}
	</div>
{:else}
	<div class="overflow-x-auto rounded-lg border border-border bg-bg-elevated">
		{#if loading}
			<table class="w-full min-w-[880px] border-collapse text-left">
				<tbody>
					{#each Array(5) as _, i (i)}
						<tr class="border-b border-border">
							<td class="px-4 py-4" colspan={HEADERS.length}>
								<div class="flex items-center gap-4">
									<div class="h-4 w-16 rounded-full bg-surface motion-safe:animate-pulse"></div>
									<div class="h-4 flex-1 rounded bg-surface motion-safe:animate-pulse"></div>
									<div class="h-4 w-24 rounded bg-surface motion-safe:animate-pulse"></div>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else if error}
			<div class="px-5 py-10 text-center">
				<p class="text-sm font-semibold text-status-failed">
					Failed to load torrents
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					{error.message || "Unknown error"}
				</p>
			</div>
		{:else if rows.length === 0}
			<div
				class="flex flex-col items-center justify-center gap-2 px-5 py-14 text-center"
			>
				<Magnet size={26} class="text-fg-faint" aria-hidden="true" />
				<p class="text-sm font-medium text-fg">No torrents yet</p>
				<p class="text-xs text-fg-muted">
					Add a magnet link or a .torrent file to get started.
				</p>
				{#if canControl}
					<button
						type="button"
						onclick={onAdd}
						class="mt-2 inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
					>
						<Magnet size={15} aria-hidden="true" />
						Add torrent
					</button>
				{/if}
			</div>
		{:else}
			<table class="w-full min-w-[880px] border-collapse text-left">
				<thead
					class="sticky top-0 z-10 bg-surface text-[10px] uppercase tracking-[0.12em] text-fg-faint"
				>
					<tr>
						{#each HEADERS as h, i (i)}
							<th
								scope="col"
								class="px-2 py-2.5 font-medium first:pl-4 last:pr-4"
							>
								{h}
							</th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each rows as t (t.hash)}
						<TorrentRow
							torrent={t}
							{density}
							selected={selectedHash === t.hash}
							{onOpen}
						/>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
{/if}
