<script lang="ts">
	import { ChevronRight, LoaderCircle } from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import IdentifyDialog from "./IdentifyDialog.svelte";
	import { api } from "../../lib/api";
	import type { PendingItem, PendingPreview } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		item,
		busy = false,
		onImport,
		onReplace,
		onIgnore,
	}: {
		item: PendingItem;
		busy?: boolean;
		onImport: () => void;
		onReplace: (removeOld: boolean) => void;
		onIgnore: (removeTorrent: boolean) => void;
	} = $props();

	let replaceOpen = $state(false);
	let ignoreOpen = $state(false);
	let identifyOpen = $state(false);
	let removeOld = $state(false);
	let removeTorrent = $state(false);

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}

	let mediaLabel = $derived.by(() => {
		const m = item.media;
		// Unmatched: the parsed title reads better than the raw release name,
		// which is shown in full on the mono line below anyway.
		if (!m) return item.parsed_title || item.title;
		if (m.type === "episode" && m.season != null && m.episode != null) {
			return `${m.title} · S${pad(m.season)}E${pad(m.episode)}`;
		}
		return m.year ? `${m.title} (${m.year})` : m.title;
	});

	// The proposal links one anchor episode, which says nothing about the rest
	// of a pack. The preview reads the torrent's own file list, so it costs a
	// download-client round trip — only worth it for an episode proposal, and
	// stale-for-a-while since a seeding torrent's contents don't change.
	const preview = createQuery<PendingPreview>(() => ({
		queryKey: ["activity", "pending", item.id, "preview"],
		queryFn: () => api<PendingPreview>(`/activity/pending/${item.id}/preview`),
		enabled: item.media?.type === "episode",
		staleTime: 5 * 60 * 1000,
		retry: false,
	}));

	let counts = $derived(preview.data);
	// A single-file torrent lands on exactly the anchor the row already names.
	let showBreakdown = $derived(
		!!counts && counts.imports.length + counts.on_disk.length > 1,
	);
	let expanded = $state(false);

	function openReplace() {
		removeOld = false;
		replaceOpen = true;
	}

	function label(e: { season: number; episode: number; title?: string }) {
		const se = `S${pad(e.season)}E${pad(e.episode)}`;
		return e.title ? `${se} · ${e.title}` : se;
	}
	function openIgnore() {
		removeTorrent = false;
		ignoreOpen = true;
	}
</script>

<article
	class="flex flex-col gap-3 rounded-lg border border-border bg-bg-card/60 p-3.5 sm:flex-row sm:items-center sm:justify-between"
>
	<div class="min-w-0">
		<div class="flex items-center gap-2">
			<p class="truncate text-sm font-semibold text-fg">{mediaLabel}</p>
			{#if item.quality}
				<span
					class="shrink-0 rounded-full bg-bg-elevated px-2 py-0.5 text-[10px] font-medium text-fg-muted"
				>
					{item.quality}
				</span>
			{/if}
		</div>
		<p class="mt-1 flex items-center gap-1.5 text-xs text-status-wanted">
			<span
				class="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-status-wanted"
				aria-hidden="true"
			></span>
			{#if showBreakdown && counts}
				{counts.imports.length} to import · {counts.on_disk.length} already on disk
			{:else}
				{item.reason}
			{/if}
		</p>
		<p class="mt-1 truncate font-mono text-[11px] text-fg-faint" title={item.title}>
			{item.title}
		</p>
		{#if showBreakdown && counts}
			<button
				type="button"
				onclick={() => (expanded = !expanded)}
				aria-expanded={expanded}
				class="mt-2 flex items-center gap-1 rounded text-[11px] font-medium text-fg-muted transition hover:text-fg focus:outline-none focus:ring-2 focus:ring-accent-ring"
			>
				<ChevronRight
					class="h-3 w-3 transition-transform {expanded ? 'rotate-90' : ''}"
					aria-hidden="true"
				/>
				{counts.imports.length} to import
			</button>
			{#if expanded}
				<ul class="mt-1.5 space-y-0.5 ps-4 text-[11px] text-fg-muted">
					{#each counts.imports as e (`${e.season}-${e.episode}`)}
						<li class="truncate">{label(e)}</li>
					{/each}
					{#if counts.on_disk.length > 0}
						<li class="pt-1 text-fg-faint">
							{counts.on_disk.length} already on disk, left in place
						</li>
					{/if}
					{#if counts.unmatched > 0}
						<li class="text-fg-faint">
							{counts.unmatched} file(s) matched no episode
						</li>
					{/if}
				</ul>
			{/if}
		{/if}
	</div>

	<div class="flex shrink-0 items-center gap-2">
		<!--
			One primary action per row. An unmatched proposal has no title to
			import into yet, so it offers Identify and nothing else; otherwise the
			choice is by whether the media already has a file: Replace (swap the
			existing file) when it does, Import (accept into the empty slot) when
			it doesn't. The inapplicable action is never shown.
		-->
		{#if !item.media}
			<button
				type="button"
				onclick={() => (identifyOpen = true)}
				disabled={busy}
				class="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
			>
				Identify
			</button>
		{:else if item.has_file}
			<button
				type="button"
				onclick={openReplace}
				disabled={busy}
				class="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if busy}
					<LoaderCircle class="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
				{/if}
				Replace
			</button>
		{:else}
			<button
				type="button"
				onclick={onImport}
				disabled={busy}
				class="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if busy}
					<LoaderCircle class="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
				{/if}
				Import
			</button>
		{/if}
		<button
			type="button"
			onclick={openIgnore}
			disabled={busy}
			class="inline-flex h-8 items-center rounded-md px-3 text-xs font-medium text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
		>
			{i18n.common_ignore()}
		</button>
	</div>
</article>

<Dialog
	open={replaceOpen}
	title={i18n.imports_replace_confirm()}
	onClose={() => (replaceOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_replace(),
			variant: "danger",
			dismiss: false,
			pending: busy,
			onClick: () => {
				onReplace(removeOld);
				replaceOpen = false;
			},
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{i18n.imports_replace_help()}
	</p>
	<Checkbox
		checked={removeOld}
		onChange={(v) => (removeOld = v)}
		class="mt-4 text-sm text-fg"
	>
		{i18n.imports_remove_old_torrent()}
	</Checkbox>
</Dialog>

<Dialog
	open={ignoreOpen}
	title={i18n.imports_ignore_confirm()}
	onClose={() => (ignoreOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.common_ignore(),
			variant: "danger",
			dismiss: false,
			pending: busy,
			onClick: () => {
				onIgnore(removeTorrent);
				ignoreOpen = false;
			},
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{i18n.imports_ignore_help()}
	</p>
	<Checkbox
		checked={removeTorrent}
		onChange={(v) => (removeTorrent = v)}
		class="mt-4 text-sm text-fg"
	>
		{i18n.file_also_remove_torrent()}
	</Checkbox>
</Dialog>

<IdentifyDialog
	open={identifyOpen}
	id={item.id}
	title={item.title}
	parsedTitle={item.parsed_title}
	onClose={() => (identifyOpen = false)}
/>
