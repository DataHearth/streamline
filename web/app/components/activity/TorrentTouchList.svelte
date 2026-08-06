<script lang="ts">
	import { Magnet } from "@lucide/svelte";
	import TouchRow from "./TouchRow.svelte";
	import { torrentMeta } from "../../lib/activity-touch";
	import type { Torrent } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { errorText } from "../../lib/api";

	// The torrent table below md. Nine columns don't survive 390px, so the name
	// takes the title line, the infohash the release line, and ↓ / ↑ / ratio /
	// swarm share the third — the rest is in the detail sheet the row opens.
	let {
		rows,
		loading,
		error = null,
		canControl = false,
		onOpen,
	}: {
		rows: Torrent[];
		loading: boolean;
		error?: Error | null;
		canControl?: boolean;
		onOpen: (hash: string) => void;
	} = $props();

	const fetching = (t: Torrent) => t.status === "fetching" && !t.name;
</script>

<div class="mt-3 md:hidden">
	{#if loading}
		<div class="overflow-hidden rounded-xl border border-border bg-bg-elevated">
			{#each Array(4) as _, i (i)}
				<div class="flex items-center gap-3 border-b border-border px-3 py-3 last:border-b-0">
					<div
						class="h-10 w-10 shrink-0 rounded-full bg-surface motion-safe:animate-pulse"
					></div>
					<div class="flex-1 space-y-1.5">
						<div class="h-3 w-2/3 rounded bg-surface motion-safe:animate-pulse"></div>
						<div class="h-2.5 w-1/2 rounded bg-surface motion-safe:animate-pulse"></div>
					</div>
				</div>
			{/each}
		</div>
	{:else if error}
		<div
			class="rounded-xl border border-status-failed/25 bg-bg-elevated px-5 py-9 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">{i18n.torrent_load_failed()}</p>
			<p class="mt-1 font-mono text-[11px] text-fg-subtle">
				{errorText(error)}
			</p>
		</div>
	{:else if rows.length === 0}
		<div
			class="flex flex-col items-center gap-1.5 rounded-xl border border-border bg-bg-elevated px-5 py-11 text-center"
		>
			<Magnet size={24} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-semibold text-fg">{i18n.torrent_none_yet()}</p>
			<p class="max-w-[17rem] text-xs text-fg-muted">
				{canControl
					? i18n.torrent_use_add()
					: i18n.activity_nothing_downloading()}
			</p>
		</div>
	{:else}
		<div class="overflow-hidden rounded-xl border border-border bg-bg-elevated">
			{#each rows as t (t.hash)}
				<TouchRow
					status={t.status}
					progress={t.progress}
					title={fetching(t) ? i18n.torrent_fetching_metadata() : t.name}
					placeholderTitle={fetching(t)}
					release={`${t.hash.slice(0, 24)}…`}
					badge={t.tracked ? undefined : "untracked"}
					meta={torrentMeta(t)}
					onOpen={() => onOpen(t.hash)}
				/>
			{/each}
		</div>
	{/if}
</div>
