<script lang="ts">
	import { Magnet, Upload, Info, FileText } from "@lucide/svelte";
	import Modal from "../modals/Modal.svelte";
	import { cn } from "../../lib/cn";
	import { isMagnet, readTorrentFile } from "../../lib/torrent-file";
	import type { AddTorrentRequest } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { errorText } from "../../lib/api";

	let {
		open,
		busy = false,
		onClose,
		onAdd,
	}: {
		open: boolean;
		busy?: boolean;
		onClose: () => void;
		onAdd: (payload: AddTorrentRequest) => void;
	} = $props();

	type Source = "magnet" | "file";
	let source = $state<Source>("magnet");
	let magnet = $state("");
	let fileName = $state("");
	let fileB64 = $state("");
	let fileErr = $state("");

	// Reset when the modal is (re)opened.
	$effect(() => {
		if (open) {
			source = "magnet";
			magnet = "";
			fileName = "";
			fileB64 = "";
			fileErr = "";
		}
	});

	let magnetValid = $derived(isMagnet(magnet));
	let canSubmit = $derived(
		!busy && (source === "magnet" ? magnetValid : !!fileB64),
	);

	async function onFile(e: Event) {
		fileErr = "";
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!f) return;
		try {
			const read = await readTorrentFile(f);
			fileName = read.name;
			fileB64 = read.base64;
		} catch (err) {
			fileErr = errorText(err, i18n.torrent_read_failed());
		}
	}

	function submit() {
		if (!canSubmit) return;
		if (source === "magnet") onAdd({ magnet: magnet.trim() });
		else onAdd({ torrent: fileB64 });
	}
</script>

<Modal {open} title={i18n.action_add_torrent()} size="lg" {onClose}>
	<!-- source switcher -->
	<div class="mb-4 grid grid-cols-2 gap-1 rounded-lg border border-border bg-bg-card p-1">
		<button
			type="button"
			onclick={() => (source = "magnet")}
			class={cn(
				"inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium transition",
				source === "magnet" ? "bg-accent-soft text-accent-text" : "text-fg-muted hover:text-fg",
			)}
		>
			<Magnet size={15} aria-hidden="true" />
			{i18n.torrent_magnet_link()}
		</button>
		<button
			type="button"
			onclick={() => (source = "file")}
			class={cn(
				"inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium transition",
				source === "file" ? "bg-accent-soft text-accent-text" : "text-fg-muted hover:text-fg",
			)}
		>
			<Upload size={15} aria-hidden="true" />
			{i18n.torrent_upload_file()}
		</button>
	</div>

	{#if source === "magnet"}
		<label class="block">
			<span class="mb-1 block text-sm font-medium text-fg">{i18n.torrent_magnet_uri()}</span>
			<textarea
				value={magnet}
				oninput={(e) => (magnet = e.currentTarget.value)}
				rows="3"
				placeholder="magnet:?xt=urn:btih:…"
				class="w-full resize-none rounded-md border border-border bg-bg px-3 py-2 font-mono text-xs text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
			></textarea>
		</label>
		{#if magnet.trim() && !magnetValid}
			<p class="mt-1 text-xs text-status-failed">
				{i18n.torrent_not_magnet_curly()}
				<code class="font-mono">magnet:?</code>).
			</p>
		{/if}
	{:else}
		<label
			class="flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-bg-card px-4 py-8 text-center transition hover:border-border-strong"
		>
			<input type="file" accept=".torrent" class="sr-only" onchange={onFile} />
			{#if fileName}
				<FileText size={22} class="text-accent-text" aria-hidden="true" />
				<span class="max-w-full truncate font-mono text-xs text-fg">{fileName}</span>
				<span class="text-[11px] text-fg-subtle">{i18n.torrent_click_change_file()}</span>
			{:else}
				<Upload size={22} class="text-fg-faint" aria-hidden="true" />
				<span class="text-sm text-fg-muted">{i18n.torrent_choose_file()}</span>
				<span class="text-[11px] text-fg-subtle">or drag it onto this area</span>
			{/if}
		</label>
		{#if fileErr}
			<p class="mt-1 text-xs text-status-failed">{fileErr}</p>
		{/if}
	{/if}

	<!-- untracked hint -->
	<div
		class="mt-4 flex items-start gap-2 rounded-md border border-status-wanted/30 bg-status-wanted/[0.06] px-3 py-2.5 text-xs text-status-wanted"
	>
		<Info size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
		<span>
			{i18n.torrent_not_linked_curly()}
			<span class="font-medium">{i18n.common_needs_attention()}</span> once it finishes.
		</span>
	</div>

	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
		>
			{i18n.common_cancel()}
		</button>
		<button
			type="button"
			onclick={submit}
			disabled={!canSubmit}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{busy ? i18n.action_adding() : i18n.action_add_torrent()}
		</button>
	{/snippet}
</Modal>
