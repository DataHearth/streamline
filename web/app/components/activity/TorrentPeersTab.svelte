<script lang="ts">
	import { Users } from "@lucide/svelte";
	import { formatSpeed } from "../../lib/format";
	import type { TorrentPeer, TorrentStatus } from "../../lib/types";

	let {
		peers,
		peerCount,
		status,
	}: {
		peers: TorrentPeer[];
		peerCount: number;
		status: TorrentStatus;
	} = $props();
</script>

{#if peers.length === 0}
	<div class="flex flex-col items-center justify-center gap-2 py-16 text-center">
		<Users size={24} class="text-fg-faint" aria-hidden="true" />
		<p class="text-sm font-medium text-fg">No peers connected</p>
		<p class="text-xs text-fg-muted">
			{status === "paused"
				? "Resume the torrent to reconnect to the swarm."
				: "The engine isn’t connected to any peers right now."}
		</p>
	</div>
{:else}
	<table class="w-full border-collapse text-left">
		<thead
			class="text-[10px] uppercase tracking-[0.12em] text-fg-faint"
		>
			<tr class="border-b border-border">
				<th scope="col" class="py-2 pr-2 font-medium">Address</th>
				<th scope="col" class="px-2 py-2 font-medium">Client</th>
				<th scope="col" class="px-2 py-2 text-right font-medium">Down</th>
				<th scope="col" class="py-2 pl-2 text-right font-medium">Up</th>
			</tr>
		</thead>
		<tbody>
			{#each peers as p, i (p.addr + i)}
				<tr class="border-b border-border/60 text-sm">
					<td class="py-2 pr-2 font-mono text-xs text-fg-muted">
						{p.addr}
					</td>
					<td class="px-2 py-2 text-xs text-fg-subtle">{p.client || "—"}</td>
					<td
						class="px-2 py-2 text-right font-mono tabular-nums text-xs"
					>
						<span class={(p.download_rate ?? 0) > 0 ? "text-status-downloading" : "text-fg-faint"}>
							{formatSpeed(p.download_rate) || "—"}
						</span>
					</td>
					<td
						class="py-2 pl-2 text-right font-mono tabular-nums text-xs"
					>
						<span class={(p.upload_rate ?? 0) > 0 ? "text-status-seeding" : "text-fg-faint"}>
							{formatSpeed(p.upload_rate) || "—"}
						</span>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
	<p class="mt-3 text-[11px] text-fg-faint">
		{peers.length} connected · {peerCount} peers in swarm
	</p>
{/if}
