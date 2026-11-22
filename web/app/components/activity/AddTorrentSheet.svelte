<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { parts } from "@lib/message-parts";
	import { FileText, Info, LoaderCircle, Upload } from "@lucide/svelte";
	import { isMagnet, readTorrentFile } from "@lib/torrent-file";
	import type { AddTorrentRequest } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { errorText } from "@lib/api";

	const notLinkedBody = parts(i18n.torrent_not_linked_body, ["place"]);

	// The add-torrent modal below md. Both inputs are on screen at once rather
	// than behind a source switcher — on a phone the magnet almost always comes
	// from the clipboard, and a two-tab control to reach a file picker is a tab
	// more than the whole form needs.
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

	let magnet = $state("");
	let fileName = $state("");
	let fileB64 = $state("");
	let fileErr = $state("");

	$effect(() => {
		if (!open) return;
		magnet = "";
		fileName = "";
		fileB64 = "";
		fileErr = "";
	});

	let magnetValid = $derived(isMagnet(magnet));
	let canSubmit = $derived(!busy && (magnetValid || !!fileB64));

	async function onFile(e: Event) {
		fileErr = "";
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!f) return;
		try {
			const read = await readTorrentFile(f);
			fileName = read.name;
			fileB64 = read.base64;
			// A file and a magnet are two different adds; the file wins the form.
			magnet = "";
		} catch (err) {
			fileErr = errorText(err, i18n.torrent_read_failed());
		}
	}

	async function paste() {
		try {
			const text = await navigator.clipboard.readText();
			if (text) {
				magnet = text.trim();
				fileName = "";
				fileB64 = "";
			}
		} catch {
			// Clipboard read denied — the field is still typeable.
		}
	}

	function submit() {
		if (!canSubmit) return;
		if (fileB64) onAdd({ torrent: fileB64 });
		else onAdd({ magnet: magnet.trim() });
	}
</script>

{#if open}
	<BottomSheet
		title={i18n.action_add_torrent()}
		label={i18n.action_add_torrent()}
		{onClose}
		hideFrom="md"
	>
		<div class="mb-2.5 eyebrow text-[9.5px] text-fg-subtle">
			{i18n.torrent_magnet_link()}
		</div>
		<div
			class="flex h-11 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
		>
			<input
				type="text"
				inputmode="url"
				autocomplete="off"
				spellcheck="false"
				value={magnet}
				oninput={(e) => {
					magnet = e.currentTarget.value;
					if (magnet) {
						fileName = "";
						fileB64 = "";
					}
				}}
				placeholder="magnet:?xt=urn:btih:…"
				aria-label={i18n.torrent_magnet_link()}
				class="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-fg outline-none placeholder:text-fg-subtle"
			/>
			<button
				type="button"
				onclick={paste}
				class="touch-hit min-w-11 shrink-0 text-center text-[12.5px] font-semibold text-accent-text transition active:opacity-70"
			>
				{i18n.common_paste()}
			</button>
		</div>
		{#if magnet.trim() && !magnetValid}
			<p class="mt-1.5 text-[11.5px] text-status-failed">
				{i18n.torrent_not_magnet_straight()}
				<code class="font-mono">magnet:?</code>.
			</p>
		{/if}

		<!-- biome-ignore lint/a11y/noLabelWithoutControl: its text is inside an {#if}, which Biome does not read -->
		<label
			class="mt-3 flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-border-strong px-4 py-6 text-center transition active:bg-surface"
		>
			<input type="file" accept=".torrent" class="sr-only" onchange={onFile} />
			{#if fileName}
				<FileText size={20} class="text-accent-text" aria-hidden="true" />
				<span class="max-w-full truncate font-mono text-[12px] text-fg">
					{fileName}
				</span>
				<span class="text-[11px] text-fg-subtle">{i18n.torrent_tap_change_file()}</span>
			{:else}
				<Upload size={20} class="text-fg-faint" aria-hidden="true" />
				<span class="text-[13px] font-semibold text-fg">{i18n.torrent_choose_file()}</span>
				<span class="text-[11px] text-fg-subtle">{i18n.torrent_or_drop()}</span>
			{/if}
		</label>
		{#if fileErr}
			<p class="mt-1.5 text-[11.5px] text-status-failed">{fileErr}</p>
		{/if}

		<div
			class="mt-3.5 flex items-start gap-2 rounded-xl border border-status-wanted/30 bg-status-wanted/[0.06] px-3 py-2.5 text-[11.5px] leading-relaxed text-status-wanted"
		>
			<Info size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<span>
				{#each notLinkedBody as p}{#if p.slot === "place"}<span class="font-semibold">{i18n.common_needs_attention()}</span>{:else}{p.text}{/if}{/each}
			</span>
		</div>
		{#snippet footer()}
			<button
				type="button"
				onclick={onClose}
				class="inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface"
			>
				{i18n.common_cancel()}
			</button>
			<button
				type="button"
				onclick={submit}
				disabled={!canSubmit}
				class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed disabled:opacity-50"
			>
				{#if busy}
					<LoaderCircle size={16} class="motion-safe:animate-spin" aria-hidden="true" />
				{/if}
				{busy ? i18n.action_adding() : i18n.action_add_torrent()}
			</button>
		{/snippet}
	</BottomSheet>
{/if}
