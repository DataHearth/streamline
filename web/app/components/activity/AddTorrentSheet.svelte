<script lang="ts">
	import { fade, fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { FileText, Info, LoaderCircle, Upload, X } from "@lucide/svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import { isMagnet, readTorrentFile } from "../../lib/torrent-file";
	import type { AddTorrentRequest } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { errorText } from "../../lib/api";

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
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
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
	<div
		class="fixed inset-0 z-50 md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.action_add_torrent()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{i18n.action_add_torrent()}</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-9 w-9 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2"
			>
				<div class="mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint">
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
						class="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-fg outline-none placeholder:text-fg-faint"
					/>
					<button
						type="button"
						onclick={paste}
						class="shrink-0 text-[12.5px] font-semibold text-accent-text transition active:opacity-70"
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
						<span class="text-[11px] text-fg-subtle">or drop it here</span>
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
						{i18n.torrent_not_linked_straight()}
						<span class="font-semibold">{i18n.common_needs_attention()}</span> once it finishes.
					</span>
				</div>
			</div>

			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
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
			</div>
		</div>
	</div>
{/if}
