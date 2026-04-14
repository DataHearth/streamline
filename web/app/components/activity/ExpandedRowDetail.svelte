<script lang="ts">
	import { slide } from "svelte/transition";
	import { Pause, Play, Ban, Trash2, LoaderCircle } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";
	import { cn } from "../../lib/cn";
	import { formatBytes } from "../../lib/format";
	import { formatDateTime } from "../../lib/dates";
	import type { QueueEntry, HistoryEntry } from "../../lib/types";

	let {
		item,
		view,
		colspan,
		busy = false,
		canControl = false,
		onCancel,
		onPause,
		onResume,
		onRemove,
	}: {
		item: QueueEntry | HistoryEntry;
		view: "queue" | "history";
		colspan: number;
		busy?: boolean;
		canControl?: boolean;
		onCancel: (id: number) => void;
		onPause: (id: number) => void;
		onResume: (id: number) => void;
		onRemove: (id: number) => void;
	} = $props();

	let confirmCancel = $state(false);
	let confirmRemove = $state(false);

	let isPaused = $derived(view === "queue" && item.status === "paused");

	type KV = { label: string; value: string };
	let rows = $derived.by<KV[]>(() => {
		const out: KV[] = [
			{ label: "Release", value: item.title },
			{ label: "Indexer", value: item.indexer || "—" },
			{ label: "Client", value: item.download_client || "—" },
			{ label: "Size", value: formatBytes(item.size) },
			{ label: "Created", value: formatDateTime(item.created_at) },
		];
		if (view === "history") {
			const h = item as HistoryEntry;
			out.push({
				label: "Imported",
				value: h.imported_at ? formatDateTime(h.imported_at) : "—",
			});
		}
		return out;
	});

	const btn =
		"inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-semibold transition disabled:opacity-50";
</script>

<tr class="bg-bg-card">
	<td {colspan} class="border-t border-border p-0">
		<!-- slide on the inner div: Svelte's slide can't animate a <tr>'s height -->
		<div
			transition:slide={{ duration: 180 }}
			class="grid gap-4 p-4 md:grid-cols-[1fr_auto]"
		>
			<dl
				class="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1.5 text-xs"
			>
				{#each rows as kv (kv.label)}
					<dt
						class="font-medium uppercase tracking-[0.1em] text-fg-faint"
					>
						{kv.label}
					</dt>
					<dd class="min-w-0 break-words font-mono text-fg-muted">
						{kv.value}
					</dd>
				{/each}
				{#if item.failure_reason}
					<dt
						class="font-medium uppercase tracking-[0.1em] text-status-failed"
					>
						Error
					</dt>
					<dd class="min-w-0 break-words font-mono text-status-failed">
						{item.failure_reason}
					</dd>
				{/if}
			</dl>

			{#if canControl}
			<div class="flex items-start gap-2">
				{#if view === "queue"}
					{#if isPaused}
						<button
							type="button"
							disabled={busy}
							onclick={() => onResume(item.id)}
							class={cn(btn, "bg-bg-subtle text-fg hover:bg-surface")}
						>
							{#if busy}
								<LoaderCircle
									size={13}
									class="motion-safe:animate-spin"
									aria-hidden="true"
								/>
							{:else}
								<Play size={13} aria-hidden="true" />
							{/if}
							Resume
						</button>
					{:else}
						<button
							type="button"
							disabled={busy}
							onclick={() => onPause(item.id)}
							class={cn(btn, "bg-bg-subtle text-fg hover:bg-surface")}
						>
							{#if busy}
								<LoaderCircle
									size={13}
									class="motion-safe:animate-spin"
									aria-hidden="true"
								/>
							{:else}
								<Pause size={13} aria-hidden="true" />
							{/if}
							Pause
						</button>
					{/if}
					<button
						type="button"
						disabled={busy}
						onclick={() => (confirmCancel = true)}
						class={cn(
							btn,
							"bg-status-failed/15 text-status-failed hover:bg-status-failed/25",
						)}
					>
						<Ban size={13} aria-hidden="true" />
						Cancel
					</button>
				{:else}
					<button
						type="button"
						disabled={busy}
						onclick={() => (confirmRemove = true)}
						class={cn(
							btn,
							"bg-status-failed/15 text-status-failed hover:bg-status-failed/25",
						)}
					>
						<Trash2 size={13} aria-hidden="true" />
						Remove
					</button>
				{/if}
			</div>
			{/if}
		</div>
	</td>
</tr>

<Dialog
	open={confirmCancel}
	title="Cancel download?"
	onClose={() => (confirmCancel = false)}
	actions={[
		{ label: "Keep", variant: "ghost", autofocus: true },
		{
			label: "Cancel download",
			variant: "danger",
			onClick: () => onCancel(item.id),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		Removes the torrent and its partial files from the client, then
		deletes <span class="font-medium text-fg">{item.title}</span> from the
		queue. The movie returns to <em>wanted</em> if it has no file yet.
	</p>
</Dialog>

<Dialog
	open={confirmRemove}
	title="Remove history record?"
	onClose={() => (confirmRemove = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{ label: "Remove", variant: "danger", onClick: () => onRemove(item.id) },
	]}
>
	<p class="text-sm text-fg-muted">
		Deletes the history entry for
		<span class="font-medium text-fg">{item.title}</span>. The movie and
		its files are not affected.
	</p>
</Dialog>
