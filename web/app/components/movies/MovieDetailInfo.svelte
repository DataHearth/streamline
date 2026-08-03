<script lang="ts">
	import { ExternalLink, Trash2 } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { auth } from "../../lib/auth.svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { Movie } from "../../lib/types";

	let {
		movie,
		qualityProfileName,
	}: {
		movie: Movie;
		qualityProfileName: string | null;
	} = $props();

	let primary = $derived(movie.media_files?.[0]);

	const qc = useQueryClient();
	let deleteOpen = $state(false);
	let removeTorrent = $state(false);

	const del = createMutation<unknown, Error, { fileId: number; remove: boolean }>(
		() => ({
			mutationFn: ({ fileId, remove }) =>
				api(`/movies/${movie.id}/files/${fileId}`, {
					method: "DELETE",
					body: { remove_torrent: remove },
				}),
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["movie", movie.id] });
				toast.ok("File deleted");
				deleteOpen = false;
			},
			onError: (e) => toast.err(e.message ?? "Delete failed"),
		}),
	);
</script>

<aside class="flex flex-col gap-4">
	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-file"
	>
		<div class="flex items-center justify-between">
			<h4
				id="info-file"
				class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
			>
				File
			</h4>
			{#if primary && auth.canAddDirectly}
				<button
					type="button"
					onclick={() => {
						removeTorrent = false;
						deleteOpen = true;
					}}
					aria-label="Delete file"
					title="Delete file"
					class="grid h-7 w-7 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
				>
					<Trash2 class="h-4 w-4" aria-hidden="true" />
				</button>
			{/if}
		</div>
		{#if primary}
			<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
				<dt class="text-fg-subtle">Resolution</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_resolution || "—"}
				</dd>
				<dt class="text-fg-subtle">Codec</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_codec || "—"}
				</dd>
				<dt class="text-fg-subtle">Source</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_source || "—"}
				</dd>
				<dt class="text-fg-subtle">Size</dt>
				<dd class="text-right font-mono text-fg">
					{formatBytes(primary.size)}
				</dd>
				<dt class="text-fg-subtle">Group</dt>
				<dd class="text-right font-mono text-fg">
					{primary.release_group || "—"}
				</dd>
				<dt class="text-fg-subtle">Path</dt>
				<dd
					class="min-w-0 truncate text-right font-mono text-fg"
					title={primary.path}
				>
					{primary.path}
				</dd>
			</dl>
		{:else}
			<p class="mt-3 text-[12px] text-fg-subtle">
				No file yet.{movie.status === "wanted"
					? " Searching nightly."
					: ""}
			</p>
		{/if}
	</section>

	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-library"
	>
		<h4
			id="info-library"
			class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
		>
			Library
		</h4>
		<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
			<dt class="text-fg-subtle">Quality profile</dt>
			<dd class="text-right font-mono text-fg">
				{qualityProfileName ?? "—"}
			</dd>
			<dt class="text-fg-subtle">Status</dt>
			<dd class="text-right font-mono text-fg capitalize">
				{movie.status}
			</dd>
			<dt class="text-fg-subtle">Monitored</dt>
			<dd class="text-right font-mono text-fg">
				{movie.monitored ? "Yes" : "No"}
			</dd>
			{#if movie.year}
				<dt class="text-fg-subtle">Year</dt>
				<dd class="text-right font-mono text-fg">{movie.year}</dd>
			{/if}
			{#if movie.runtime}
				<dt class="text-fg-subtle">Runtime</dt>
				<dd class="text-right font-mono text-fg">{movie.runtime}m</dd>
			{/if}
			<dt class="text-fg-subtle">TMDB</dt>
			<dd class="text-right">
				<a
					href="https://www.themoviedb.org/movie/{movie.tmdb_id}"
					target="_blank"
					rel="noopener noreferrer"
					class="inline-flex items-center gap-1 font-mono text-accent-text transition hover:text-accent"
				>
					{movie.tmdb_id}
					<ExternalLink size={11} aria-hidden="true" />
				</a>
			</dd>
		</dl>
	</section>
</aside>

<Dialog
	open={deleteOpen}
	title="Delete this file?"
	onClose={() => (deleteOpen = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete file",
			variant: "danger",
			dismiss: false,
			pending: del.isPending,
			onClick: () =>
				primary && del.mutate({ fileId: primary.id, remove: removeTorrent }),
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
