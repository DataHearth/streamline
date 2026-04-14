<script lang="ts">
	import { Film, Trash2 } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { auth } from "../../lib/auth.svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import EmptyCard from "./EmptyCard.svelte";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { MediaFile } from "../../lib/types";

	let { files, movieId }: { files: MediaFile[]; movieId: number } = $props();

	const qc = useQueryClient();
	let deleteTarget = $state<MediaFile | null>(null);
	let removeTorrent = $state(false);

	const del = createMutation<unknown, Error, { fileId: number; remove: boolean }>(
		() => ({
			mutationFn: ({ fileId, remove }) =>
				api(`/movies/${movieId}/files/${fileId}`, {
					method: "DELETE",
					body: { remove_torrent: remove },
				}),
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["movie", movieId] });
				toast.ok("File deleted");
				deleteTarget = null;
			},
			onError: (e) => toast.err(e.message ?? "Delete failed"),
		}),
	);

	function openDelete(f: MediaFile) {
		deleteTarget = f;
		removeTorrent = false;
	}

	function fmtSize(bytes: number): string {
		const units = ["B", "KB", "MB", "GB", "TB"];
		let n = bytes;
		let u = 0;
		while (n >= 1024 && u < units.length - 1) {
			n /= 1024;
			u++;
		}
		return `${n.toFixed(n < 10 ? 1 : 0)} ${units[u]}`;
	}
</script>

{#snippet startImport()}
	<a
		href="/library/imports"
		class="text-xs font-medium text-accent hover:underline"
	>
		Start an import →
	</a>
{/snippet}

{#if files.length === 0}
	<EmptyCard
		icon={Film}
		title="No files imported yet."
		action={auth.isAdmin ? startImport : undefined}
	/>
{:else}
	<div class="flex flex-col gap-2">
		{#each files as f (f.id)}
			<article class="rounded-lg border border-border bg-bg-card/60 p-3">
				<div class="flex items-baseline justify-between gap-3">
					<p
						class="truncate text-sm font-semibold text-fg"
						title={f.path}
					>
						{f.path.split("/").pop()}
					</p>
					<div class="flex shrink-0 items-center gap-2">
						<span
							class="text-xs font-mono tabular-nums text-fg-muted"
						>
							{fmtSize(f.size)}
						</span>
						{#if auth.canAddDirectly}
							<button
								type="button"
								onclick={() => openDelete(f)}
								aria-label="Delete file"
								title="Delete file"
								class="grid h-8 w-8 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
							>
								<Trash2 class="h-4 w-4" aria-hidden="true" />
							</button>
						{/if}
					</div>
				</div>
				<p
					class="mt-1 truncate font-mono text-xs text-fg-faint"
					title={f.path}
				>
					{f.path}
				</p>
				<div class="mt-2 flex flex-wrap gap-1.5">
					{#if f.release_group}
						<span
							class="rounded-full bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-fg-muted"
						>
							{f.release_group}
						</span>
					{/if}
					{#if f.parsed_resolution}
						<span
							class="rounded-full bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-fg-muted"
						>
							{f.parsed_resolution}
						</span>
					{/if}
					{#if f.parsed_codec}
						<span
							class="rounded-full bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-fg-muted"
						>
							{f.parsed_codec}
						</span>
					{/if}
					{#if f.parsed_source}
						<span
							class="rounded-full bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-fg-muted"
						>
							{f.parsed_source}
						</span>
					{/if}
				</div>
			</article>
		{/each}
	</div>
{/if}

<Dialog
	open={deleteTarget !== null}
	title="Delete this file?"
	onClose={() => (deleteTarget = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete file",
			variant: "danger",
			dismiss: false,
			pending: del.isPending,
			onClick: () =>
				deleteTarget &&
				del.mutate({ fileId: deleteTarget.id, remove: removeTorrent }),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		The file is removed from disk and the movie reverts to <span
			class="font-medium text-fg">wanted</span
		>, so the next monitored search re-grabs it.
	</p>
	<Checkbox
		checked={removeTorrent}
		onChange={(v) => (removeTorrent = v)}
		class="mt-4 text-sm text-fg"
	>
		Also remove the torrent from the download client
	</Checkbox>
</Dialog>
