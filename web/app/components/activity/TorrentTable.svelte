<script lang="ts">
	import {
		Magnet,
		Zap,
		ArrowUpRight,
		ChevronUp,
		ChevronDown,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import TorrentRow from "./TorrentRow.svelte";
	import type { Torrent } from "../../lib/types";

	let {
		rows,
		loading,
		error = null,
		notConfigured = false,
		canControl = false,
		selectedHash = null,
		onOpen,
	}: {
		rows: Torrent[];
		loading: boolean;
		error?: Error | null;
		notConfigured?: boolean;
		canControl?: boolean;
		selectedHash?: string | null;
		onOpen: (hash: string) => void;
	} = $props();

	type SortKey =
		| "status"
		| "name"
		| "progress"
		| "size"
		| "download_speed"
		| "upload_speed"
		| "ratio"
		| "seeds";

	// `grow` marks the column that absorbs the leftover width. Paired with
	// `max-w-0` on the cell it lets the name truncate instead of forcing the
	// table past its container and spawning a horizontal scrollbar.
	const HEADERS: {
		label: string;
		key?: SortKey;
		numeric?: boolean;
		grow?: boolean;
	}[] = [
		{ label: "Status", key: "status" },
		{ label: "Name", key: "name", grow: true },
		{ label: "Progress", key: "progress", numeric: true },
		{ label: "Size", key: "size", numeric: true },
		{ label: "Down", key: "download_speed", numeric: true },
		{ label: "Up", key: "upload_speed", numeric: true },
		{ label: "Ratio", key: "ratio", numeric: true },
		{ label: "Seeds/Peers", key: "seeds", numeric: true },
		{ label: "" },
	];

	// Default order: what needs attention first. Live transfers on top, then the
	// stuck ones, then anything merely seeding or done — progress descending
	// within each group, so the nearly-finished sit above the just-started.
	const STATUS_RANK: Record<string, number> = {
		downloading: 0,
		stalled: 1,
		paused: 2,
		seeding: 3,
		completed: 4,
	};

	let sort = $state<SortKey>("status");
	let order = $state<"asc" | "desc">("asc");

	function toggleSort(key: SortKey, numeric: boolean) {
		if (sort === key) {
			order = order === "asc" ? "desc" : "asc";
			return;
		}
		sort = key;
		// Numbers read best largest-first; names and statuses ascending.
		order = numeric ? "desc" : "asc";
	}

	function compare(a: Torrent, b: Torrent): number {
		if (sort === "status") {
			const d = (STATUS_RANK[a.status] ?? 9) - (STATUS_RANK[b.status] ?? 9);
			if (d !== 0) return d;
			return (b.progress ?? 0) - (a.progress ?? 0);
		}
		if (sort === "name") return a.name.localeCompare(b.name);
		return ((a[sort] as number) ?? 0) - ((b[sort] as number) ?? 0);
	}

	let sortedRows = $derived.by(() => {
		const out = [...rows].sort(compare);
		return order === "desc" ? out.reverse() : out;
	});
</script>

{#if notConfigured}
	<!-- /torrents 404s when no built-in client is configured — nudge, not a
	     broken table. -->
	<div
		class="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border bg-bg-elevated px-5 py-14 text-center"
	>
		<div
			class="grid h-12 w-12 place-items-center rounded-full bg-accent-soft text-accent"
		>
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
			<table class="w-full min-w-[700px] border-collapse text-left">
				<tbody>
					{#each Array(5) as _, i (i)}
						<tr class="border-b border-border">
							<td class="px-4 py-4" colspan={HEADERS.length}>
								<div class="flex items-center gap-4">
									<div
										class="h-4 w-16 rounded-full bg-surface motion-safe:animate-pulse"
									></div>
									<div
										class="h-4 flex-1 rounded bg-surface motion-safe:animate-pulse"
									></div>
									<div
										class="h-4 w-24 rounded bg-surface motion-safe:animate-pulse"
									></div>
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
					{canControl
						? "Use Add torrent above to paste a magnet link or upload a .torrent file."
						: "Nothing is downloading right now."}
				</p>
			</div>
		{:else}
			<table class="w-full min-w-[700px] border-collapse text-left">
				<thead
					class="sticky top-0 z-10 bg-surface text-[10px] uppercase tracking-[0.12em] text-fg-faint"
				>
					<tr>
						{#each HEADERS as h, i (i)}
							<th
								scope="col"
								aria-sort={h.key && sort === h.key
									? order === "asc"
										? "ascending"
										: "descending"
									: undefined}
								class={cn(
									"px-2 py-2.5 font-medium first:pl-4 last:pr-4",
									// Every other column shrinks to its content so the
									// name column keeps the remainder.
									h.grow ? "w-full max-w-0" : "w-px whitespace-nowrap",
								)}
							>
								{#if h.key}
									{@const key = h.key}
									<button
										type="button"
										onclick={() => toggleSort(key, h.numeric ?? false)}
										class={cn(
											"-mx-1 inline-flex items-center gap-1 whitespace-nowrap rounded px-1 py-0.5 uppercase tracking-[0.12em] transition hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
											sort === h.key && "text-accent-text",
										)}
									>
										{h.label}
										{#if sort === h.key}
											{#if order === "asc"}
												<ChevronUp
													size={11}
													class="shrink-0"
													aria-hidden="true"
												/>
											{:else}
												<ChevronDown
													size={11}
													class="shrink-0"
													aria-hidden="true"
												/>
											{/if}
										{/if}
									</button>
								{:else}
									{h.label}
								{/if}
							</th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each sortedRows as t (t.hash)}
						<TorrentRow torrent={t} selected={selectedHash === t.hash} {onOpen} />
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
{/if}
