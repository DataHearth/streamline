<script lang="ts">
	import { LoaderCircle } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { PendingItem } from "../../lib/types";
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
	let removeOld = $state(false);
	let removeTorrent = $state(false);

	function pad(n: number): string {
		return String(n).padStart(2, "0");
	}

	let mediaLabel = $derived.by(() => {
		const m = item.media;
		if (!m) return item.title;
		if (m.type === "episode" && m.season != null && m.episode != null) {
			return `${m.title} · S${pad(m.season)}E${pad(m.episode)}`;
		}
		return m.year ? `${m.title} (${m.year})` : m.title;
	});

	function openReplace() {
		removeOld = false;
		replaceOpen = true;
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
			{item.reason}
		</p>
		<p class="mt-1 truncate font-mono text-[11px] text-fg-faint" title={item.title}>
			{item.title}
		</p>
	</div>

	<div class="flex shrink-0 items-center gap-2">
		<!--
			One primary action per row, chosen by whether the media already has a
			file: Replace (swap the existing file) when it does, Import (accept into
			the empty slot) when it doesn't. The inapplicable action is never shown.
		-->
		{#if item.has_file}
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
