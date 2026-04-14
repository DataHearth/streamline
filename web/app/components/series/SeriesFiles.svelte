<script lang="ts">
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { ChevronRight, Folder, Film, Trash2 } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { cn } from "../../lib/cn";
	import { auth } from "../../lib/auth.svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { Episode, Season } from "../../lib/types";

	let {
		seasons,
		seriesId,
	}: { seasons: Season[]; seriesId: number } = $props();

	const qc = useQueryClient();
	// A delete acts on one file (single episode) or many (a season / the whole
	// series); the target carries the label for the confirm copy plus the list
	// of episodes whose files get removed.
	let target = $state<{ label: string; episodes: Episode[] } | null>(null);
	let removeTorrent = $state(false);

	const del = createMutation<unknown, Error, { episodes: Episode[]; remove: boolean }>(
		() => ({
			// ponytail: sequential per-episode DELETEs (no bulk endpoint). Fine for
			// a season; add DELETE /series/{id}/files if a fully-downloaded series
			// makes this too slow.
			mutationFn: async ({ episodes, remove }) => {
				for (const ep of episodes) {
					await api(`/series/${seriesId}/episodes/${ep.id}/file`, {
						method: "DELETE",
						body: { remove_torrent: remove },
					});
				}
			},
			onSuccess: (_d, { episodes }) => {
				qc.invalidateQueries({ queryKey: ["series", seriesId] });
				toast.ok(episodes.length > 1 ? "Files deleted" : "File deleted");
				target = null;
			},
			onError: (e) => toast.err(e.message ?? "Delete failed"),
		}),
	);

	function openDelete(label: string, episodes: Episode[]) {
		target = { label, episodes };
		removeTorrent = false;
	}

	// Episodes expose quality/size from their primary media file; a path field
	// is not surfaced on the Episode schema yet, so files are identified by
	// episode code rather than on-disk filename.
	function withFiles(s: Season): Episode[] {
		return (s.episodes ?? []).filter((e) => (e.size ?? 0) > 0);
	}
	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}
	function formatBytes(bytes: number): string {
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		return `${(bytes / 1_048_576).toFixed(0)} MB`;
	}
	function seasonLabel(s: Season): string {
		return s.number === 0 ? "Specials" : `Season ${pad(s.number)}`;
	}
	function seasonSize(eps: Episode[]): number {
		return eps.reduce((n, e) => n + (e.size ?? 0), 0);
	}

	let open = $state(new Set<number>());
	function toggle(n: number) {
		const next = new Set(open);
		if (next.has(n)) next.delete(n);
		else next.add(n);
		open = next;
	}

	let allWithFiles = $derived(seasons.flatMap(withFiles));
	let totalFiles = $derived(allWithFiles.length);
	let totalSize = $derived(seasonSize(allWithFiles));
	let dialogTitle = $derived(
		!target
			? ""
			: target.episodes.length > 1
				? `Delete all ${target.episodes.length} files in ${target.label}?`
				: "Delete this episode file?",
	);
</script>

{#if totalFiles === 0}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-card/40 py-14 text-center"
	>
		<Film class="mx-auto mb-3 h-10 w-10 text-fg-faint" aria-hidden="true" />
		<p class="text-sm font-medium text-fg-muted">No files on disk yet</p>
		<p class="mt-1 text-xs text-fg-subtle">
			Episode files appear here once releases are grabbed and imported.
		</p>
	</div>
{:else}
	<div
		class="mb-3 flex items-center justify-between gap-3 px-1 text-xs text-fg-muted"
	>
		<span class="font-mono tabular">
			{totalFiles} files <span class="text-fg-faint">·</span>
			{formatBytes(totalSize)}
		</span>
		{#if auth.canAddDirectly}
			<button
				type="button"
				onclick={() => openDelete("this series", allWithFiles)}
				class="inline-flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 font-medium text-fg-muted transition hover:border-status-failed/40 hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
			>
				<Trash2 class="h-3.5 w-3.5" aria-hidden="true" />
				Delete all files
			</button>
		{/if}
	</div>
	<div class="flex flex-col gap-2">
		{#each seasons as s (s.number)}
			{@const eps = withFiles(s)}
			{@const isOpen = open.has(s.number)}
			<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
				<div class="flex items-center transition hover:bg-surface">
					<button
						type="button"
						onclick={() => toggle(s.number)}
						aria-expanded={isOpen}
						class="flex flex-1 items-center gap-3 px-4 py-3 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
					>
						<ChevronRight
							class={cn(
								"h-4 w-4 shrink-0 text-fg-muted transition-transform",
								isOpen && "rotate-90",
							)}
							aria-hidden="true"
						/>
						<Folder class="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
						<span class="font-mono text-sm text-fg">
							{seasonLabel(s)}
							{#if s.name && s.number !== 0}
								<span class="text-fg-subtle">· {s.name}</span>
							{/if}
						</span>
						<span class="ml-auto font-mono text-xs text-fg-muted">
							{eps.length}/{s.total ?? 0} ep
							<span class="text-fg-faint">·</span>
							{formatBytes(seasonSize(eps))}
						</span>
					</button>
					{#if auth.canAddDirectly && eps.length > 0}
						<button
							type="button"
							onclick={() => openDelete(seasonLabel(s), eps)}
							aria-label="Delete all files in {seasonLabel(s)}"
							title="Delete files"
							class="mr-2 grid h-8 w-8 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
						>
							<Trash2 class="h-4 w-4" aria-hidden="true" />
						</button>
					{/if}
				</div>
				{#if isOpen}
					<div transition:slide={{ duration: 200, easing: cubicOut }}>
						{#if eps.length === 0}
							<p class="border-t border-border px-4 py-3 font-mono text-xs text-fg-faint">
								no files on disk
							</p>
						{:else}
							<ul class="border-t border-border">
								{#each eps as ep (ep.id)}
									<li
										class="flex items-center gap-3 border-b border-border px-4 py-2.5 last:border-b-0"
									>
										<span
											class="grid h-7 w-9 shrink-0 place-items-center rounded bg-bg-card font-mono text-[10px] text-fg-muted"
										>
											MKV
										</span>
										<div class="min-w-0 flex-1">
											<div class="truncate text-sm text-fg">
												{ep.title || `Episode ${ep.number}`}
											</div>
											<div class="font-mono text-[11px] text-fg-muted">
												S{pad(s.number)}E{pad(ep.number)}
												{#if ep.quality}
													<span class="text-fg-faint">·</span>
													{ep.quality}
												{/if}
											</div>
										</div>
										<span class="shrink-0 font-mono text-xs tabular text-fg-muted">
											{formatBytes(ep.size ?? 0)}
										</span>
										{#if auth.canAddDirectly}
											<button
												type="button"
												onclick={() =>
													openDelete(`S${pad(s.number)}E${pad(ep.number)}`, [ep])}
												aria-label="Delete file"
												title="Delete file"
												class="grid h-8 w-8 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
											>
												<Trash2 class="h-4 w-4" aria-hidden="true" />
											</button>
										{/if}
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				{/if}
			</section>
		{/each}
	</div>
{/if}

<Dialog
	open={target !== null}
	title={dialogTitle}
	onClose={() => (target = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: target && target.episodes.length > 1 ? "Delete files" : "Delete file",
			variant: "danger",
			dismiss: false,
			pending: del.isPending,
			onClick: () =>
				target &&
				del.mutate({ episodes: target.episodes, remove: removeTorrent }),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{#if target && target.episodes.length > 1}
			The files are removed from disk and their episodes revert to <span
				class="font-medium text-fg">wanted</span
			>, so the next monitored search re-grabs them.
		{:else}
			The file is removed from disk and the episode reverts to <span
				class="font-medium text-fg">wanted</span
			>, so the next monitored search re-grabs it.
		{/if}
	</p>
	<Checkbox
		checked={removeTorrent}
		onChange={(v) => (removeTorrent = v)}
		class="mt-4 text-sm text-fg"
	>
		Also remove the torrent{target && target.episodes.length > 1 ? "s" : ""} from
		the download client
	</Checkbox>
</Dialog>
