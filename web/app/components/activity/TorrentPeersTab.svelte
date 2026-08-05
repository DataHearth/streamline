<script lang="ts">
	import { ChevronDown, ChevronUp, Users } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
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

	type SortKey = "addr" | "client" | "download_rate" | "upload_rate";

	const COLS: {
		key: SortKey;
		label: string;
		numeric?: boolean;
		right?: boolean;
		// The address absorbs the leftover width and truncates; everything else holds
		// its content. Client drops out on a narrow sheet — it was squeezing the two
		// rate columns, and a clipped "1.2 MB…" is worth less than the peer's name.
		grow?: boolean;
		hide?: string;
	}[] = [
		{ key: "addr", label: "Address", grow: true },
		{ key: "client", label: "Client", hide: "hidden @sm:table-cell" },
		{ key: "download_rate", label: "Down", numeric: true, right: true },
		{ key: "upload_rate", label: "Up", numeric: true, right: true },
	];

	// Fastest first: the swarm's useful members are the question this table
	// answers, and a list ordered by address answers nothing.
	let sort = $state<SortKey>("download_rate");
	let order = $state<"asc" | "desc">("desc");

	function toggle(key: SortKey, numeric: boolean) {
		if (sort === key) {
			order = order === "asc" ? "desc" : "asc";
			return;
		}
		sort = key;
		order = numeric ? "desc" : "asc";
	}

	let sorted = $derived.by(() => {
		const out = [...peers].sort((a, b) => {
			if (sort === "addr") return a.addr.localeCompare(b.addr);
			if (sort === "client") return (a.client ?? "").localeCompare(b.client ?? "");
			return ((a[sort] as number) ?? 0) - ((b[sort] as number) ?? 0);
		});
		return order === "desc" ? out.reverse() : out;
	});
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
	<div class="@container">
	<table class="w-full border-collapse text-left">
		<thead class="text-[10px] uppercase tracking-[0.12em] text-fg-faint">
			<tr class="border-b border-border">
				{#each COLS as c (c.key)}
					<th
						scope="col"
						aria-sort={sort === c.key
							? order === "asc"
								? "ascending"
								: "descending"
							: undefined}
						class={cn(
							"py-2 font-medium first:pr-2 last:pl-2 [&:not(:first-child):not(:last-child)]:px-2",
							c.right ? "text-right" : "text-left",
							c.grow ? "w-full max-w-0" : "w-px whitespace-nowrap",
							c.hide,
						)}
					>
						<button
							type="button"
							onclick={() => toggle(c.key, c.numeric ?? false)}
							class={cn(
								"-mx-1 inline-flex items-center gap-1 whitespace-nowrap rounded px-1 py-0.5 uppercase tracking-[0.12em] transition hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
								sort === c.key && "text-accent-text",
							)}
						>
							{c.label}
							{#if sort === c.key}
								{#if order === "asc"}
									<ChevronUp size={11} class="shrink-0" aria-hidden="true" />
								{:else}
									<ChevronDown size={11} class="shrink-0" aria-hidden="true" />
								{/if}
							{/if}
						</button>
					</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each sorted as p, i (p.addr + i)}
				<tr class="border-b border-border/60 text-sm">
					<td class="w-full max-w-0 truncate py-2 pr-2 font-mono text-xs text-fg-muted">
						{p.addr}
					</td>
					<td
						class="hidden max-w-[9rem] truncate px-2 py-2 text-xs text-fg-subtle @sm:table-cell"
					>
						{p.client || "—"}
					</td>
					<!-- whitespace-nowrap: the rate and its unit are one value, and a narrow
					     column was breaking "1.2 MB/s" across two lines. -->
					<td
						class="w-px whitespace-nowrap px-2 py-2 text-right font-mono text-xs tabular-nums"
					>
						<span
							class={(p.download_rate ?? 0) > 0
								? "text-status-downloading"
								: "text-fg-faint"}
						>
							{formatSpeed(p.download_rate) || "—"}
						</span>
					</td>
					<td
						class="w-px whitespace-nowrap py-2 pl-2 text-right font-mono text-xs tabular-nums"
					>
						<span
							class={(p.upload_rate ?? 0) > 0 ? "text-status-seeding" : "text-fg-faint"}
						>
							{formatSpeed(p.upload_rate) || "—"}
						</span>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
	</div>
	<p class="mt-3 text-[11px] text-fg-faint">
		{peers.length} connected · {peerCount} peers in swarm
	</p>
{/if}
